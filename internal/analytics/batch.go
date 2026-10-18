// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package analytics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Michad/tilegroxy/internal/static"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	// The default, since delaying tile responses is worse than losing usage data
	OnFullDrop = "drop"
	// Applies backpressure to the request goroutine
	OnFullBlock = "block"
)

var AllOnFull = []string{OnFullDrop, OnFullBlock}

//nolint:mnd
const (
	batchDefaultMaxSize   = 1000
	batchDefaultMaxAge    = 10
	batchDefaultQueueSize = 10000
	batchDefaultWorkers   = 1
	// Ticks finer than MaxAge so a partial batch flushes close to its configured age
	batchTickDivisor = 4
	// Rate limits the drop log so a saturated queue doesn't log once per request
	dropLogInterval = 30 * time.Second
)

type BatchConfig struct {
	// Flush once this many events have accumulated
	MaxSize uint
	// Seconds before a partial batch flushes, so low-traffic layers still report promptly
	MaxAge uint
	// Capacity of the queue between the request path and the flush workers
	QueueSize uint
	// Concurrent flushes
	Workers uint
	// "drop" or "block"
	OnFull string
}

// Called from a worker goroutine, one call at a time per worker, and may block
type FlushFunc func(ctx context.Context, events []analytics.Event) error

// Decouples recording an event from writing it. Add runs on the request path while a worker pool does the I/O
type Batcher struct {
	cfg   BatchConfig
	flush FlushFunc
	// For log messages
	id string

	queue chan analytics.Event
	// Closed to signal workers to drain and exit
	done     chan struct{}
	workerWG sync.WaitGroup

	closeOnce sync.Once
	// A flag isn't enough: a producer descheduled after checking it could send after draining stops, stranding the event uncounted
	closeMutex sync.RWMutex
	closed     bool

	dropped        atomic.Uint64
	lastDropLogNS  atomic.Int64
	droppedCounter metric.Int64Counter
	recordCounter  metric.Int64Counter
	errorCounter   metric.Int64Counter
}

// Fills in unset values and validates, so every module reports the same defaults and errors
func ApplyBatchDefaults(cfg BatchConfig, errorMessages config.ErrorMessages) (BatchConfig, error) {
	if cfg.MaxSize == 0 {
		cfg.MaxSize = batchDefaultMaxSize
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = batchDefaultMaxAge
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = batchDefaultQueueSize
	}
	if cfg.Workers == 0 {
		cfg.Workers = batchDefaultWorkers
	}
	if cfg.OnFull == "" {
		cfg.OnFull = OnFullDrop
	}

	if cfg.OnFull != OnFullDrop && cfg.OnFull != OnFullBlock {
		return cfg, fmt.Errorf(errorMessages.EnumError, "analytics.batch.onfull", cfg.OnFull, AllOnFull)
	}

	return cfg, nil
}

func NewBatcher(id string, cfg BatchConfig, flush FlushFunc) (*Batcher, error) {
	meter := otel.Meter(static.GetPackage())

	recordCounter, err1 := meter.Int64Counter("tilegroxy.analytics.recorded", metric.WithDescription("Number of analytics events successfully written to a destination"))
	droppedCounter, err2 := meter.Int64Counter("tilegroxy.analytics.dropped", metric.WithDescription("Number of analytics events discarded because the queue was full"))
	errorCounter, err3 := meter.Int64Counter("tilegroxy.analytics.error", metric.WithDescription("Number of analytics batches that failed to write"))

	b := &Batcher{
		cfg:            cfg,
		flush:          flush,
		id:             id,
		queue:          make(chan analytics.Event, cfg.QueueSize),
		done:           make(chan struct{}),
		droppedCounter: droppedCounter,
		recordCounter:  recordCounter,
		errorCounter:   errorCounter,
	}

	for range cfg.Workers {
		b.workerWG.Add(1)
		go b.work()
	}

	return b, errors.Join(err1, err2, err3)
}

// Never blocks under OnFullDrop. Under OnFullBlock, waits for space or context cancellation
func (b *Batcher) Add(ctx context.Context, event analytics.Event) error {
	// Held across the send so Close can't retire the queue under an accepted event
	b.closeMutex.RLock()
	defer b.closeMutex.RUnlock()

	if b.closed {
		return errors.New("analytics batcher is closed")
	}

	if b.cfg.OnFull == OnFullBlock {
		select {
		case b.queue <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-b.done:
			return errors.New("analytics batcher is closed")
		}
	}

	select {
	case b.queue <- event:
		return nil
	default:
		b.noteDropped(ctx)
		return nil
	}
}

// Rate limits the log so a persistently full queue doesn't log once per request
func (b *Batcher) noteDropped(ctx context.Context) {
	total := b.dropped.Add(1)
	b.droppedCounter.Add(ctx, 1)

	now := time.Now().UnixNano()
	last := b.lastDropLogNS.Load()

	if now-last >= int64(dropLogInterval) && b.lastDropLogNS.CompareAndSwap(last, now) {
		slog.WarnContext(ctx, fmt.Sprintf("Analytics module %v dropped an event because its queue is full (%v dropped so far). Consider raising batch.queueSize or batch.workers, or setting batch.onFull to block.", b.id, total))
	}
}

func (b *Batcher) work() {
	defer b.workerWG.Done()

	buf := make([]analytics.Event, 0, b.cfg.MaxSize)

	interval := time.Duration(b.cfg.MaxAge) * time.Second / batchTickDivisor // #nosec G115 -- operator-supplied max age in seconds, far below int64 overflow range
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	oldest := time.Time{}
	maxAge := time.Duration(b.cfg.MaxAge) * time.Second // #nosec G115 -- operator-supplied max age in seconds, far below int64 overflow range

	for {
		select {
		case event := <-b.queue:
			if len(buf) == 0 {
				oldest = time.Now()
			}

			buf = append(buf, event)

			if uint(len(buf)) >= b.cfg.MaxSize {
				b.doFlush(buf)
				buf = buf[:0]
				// A later partial batch ages from its own first event, not this one's
				oldest = time.Now()
			}
		case <-ticker.C:
			if len(buf) > 0 && time.Since(oldest) >= maxAge {
				b.doFlush(buf)
				buf = buf[:0]
				oldest = time.Now()
			}
		case <-b.done:
			// Drain before exiting so Close doesn't lose events
			for {
				select {
				case event := <-b.queue:
					buf = append(buf, event)

					if uint(len(buf)) >= b.cfg.MaxSize {
						b.doFlush(buf)
						buf = buf[:0]
					}
				default:
					if len(buf) > 0 {
						b.doFlush(buf)
					}
					return
				}
			}
		}
	}
}

func (b *Batcher) doFlush(events []analytics.Event) {
	if len(events) == 0 {
		return
	}

	// The originating request contexts may already be cancelled
	ctx := pkg.BackgroundContext()

	// buf is reused for the next batch as soon as this returns
	batch := make([]analytics.Event, len(events))
	copy(batch, events)

	err := b.runFlush(ctx, batch)

	if err != nil {
		b.errorCounter.Add(ctx, int64(len(batch)))
		slog.WarnContext(ctx, fmt.Sprintf("Analytics module %v failed to write a batch of %v events: %v", b.id, len(batch), err))
		return
	}

	b.recordCounter.Add(ctx, int64(len(batch)))
}

// Recovers panics so a broken destination fails the batch instead of crashing the process
func (b *Batcher) runFlush(ctx context.Context, batch []analytics.Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, fmt.Sprintf("Analytics module %v panicked while flushing a batch of %v events", b.id, len(batch)), "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic during flush: %v", r)
		}
	}()

	return b.flush(ctx, batch)
}

// Stops accepting events, drains the queue and does a final flush. Returns when workers finish or ctx is cancelled
func (b *Batcher) Close(ctx context.Context) error {
	// The write lock waits for in-flight Adds so the drain sees everything. Races ctx since OnFullBlock producers can park indefinitely
	sealed := make(chan struct{})

	go func() {
		defer close(sealed)

		b.closeOnce.Do(func() {
			b.closeMutex.Lock()
			b.closed = true
			close(b.done)
			b.closeMutex.Unlock()
		})
	}()

	select {
	case <-sealed:
	case <-ctx.Done():
		slog.ErrorContext(ctx, fmt.Sprintf("Dropped %v queued analytics events due to shutdown deadline expiration. Consider increasing shutdown timeouts or decreasing batch sizes to avoid further losses", len(b.queue)))
		return fmt.Errorf("analytics module %v did not stop accepting events before shutdown deadline: %w", b.id, ctx.Err())
	}

	finished := make(chan struct{})

	go func() {
		b.workerWG.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		if dropped := b.dropped.Load(); dropped > 0 {
			slog.WarnContext(ctx, fmt.Sprintf("Analytics module %v dropped %v events over its lifetime due to a full queue", b.id, dropped))
		}
		return nil
	case <-ctx.Done():
		slog.WarnContext(ctx, fmt.Sprintf("Dropped %v queued analytics events due to shutdown deadline expiration. Consider increasing shutdown timeouts or decreasing batch sizes to avoid further losses", len(b.queue)))
		return fmt.Errorf("analytics module %v did not finish flushing before shutdown deadline: %w", b.id, ctx.Err())
	}
}
