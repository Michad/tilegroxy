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
	"sync"
	"sync/atomic"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/static"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Behavior when the queue is full.
const (
	// OnFullDrop discards the event. The default; delaying tile responses is a worse outcome than
	// losing usage data
	OnFullDrop = "drop"
	// OnFullBlock waits for room, applying backpressure to the request goroutine
	OnFullBlock = "block"
)

var AllOnFull = []string{OnFullDrop, OnFullBlock}

//nolint:mnd
const (
	batchDefaultMaxSize   = 1000
	batchDefaultMaxAge    = 10
	batchDefaultQueueSize = 10000
	batchDefaultWorkers   = 1
	// How often a worker with a partial batch checks whether it has aged out. Finer than MaxAge so the
	// flush lands reasonably close to the configured age
	batchTickDivisor = 4
	// Dropped events are logged at most this often so a saturated queue doesn't log once per request
	dropLogInterval = 30 * time.Second
)

// BatchConfig controls how a module buffers events before writing them
type BatchConfig struct {
	// Flush once this many events have accumulated
	MaxSize uint
	// Flush a partial batch after this many seconds so low-traffic layers still report promptly
	MaxAge uint
	// Capacity of the in-memory queue between the request path and the flush workers
	QueueSize uint
	// How many flushes may run concurrently
	Workers uint
	// What to do when the queue is full, either "drop" or "block"
	OnFull string
}

// FlushFunc writes a batch of events to the destination. Called from a worker goroutine, one call at a time
// per worker, and may block
type FlushFunc func(ctx context.Context, events []Event) error

// Batcher decouples recording an event from writing it. Modules call Add on the request path and a pool of
// workers performs the actual I/O
type Batcher struct {
	cfg   BatchConfig
	flush FlushFunc
	// Identifies the owning module in log messages
	id string

	queue chan Event
	// Closed to signal workers to drain and exit
	done     chan struct{}
	workerWG sync.WaitGroup

	closeOnce sync.Once
	// Held for reading by Add and for writing by Close. Checking a flag wouldn't be enough on its own; a
	// producer that passed the check could still be descheduled and send after the workers stopped
	// draining, stranding the event in the queue where it counts as neither recorded nor dropped
	closeMutex sync.RWMutex
	closed     bool

	dropped        atomic.Uint64
	lastDropLogNS  atomic.Int64
	droppedCounter metric.Int64Counter
	recordCounter  metric.Int64Counter
	errorCounter   metric.Int64Counter
}

// ApplyBatchDefaults fills in unset values and validates the result. Modules call this from Initialize so
// every module reports the same defaults and the same errors
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
		queue:          make(chan Event, cfg.QueueSize),
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

// Add enqueues an event. Under the default OnFullDrop it never blocks; under OnFullBlock it waits for queue
// space or for the supplied context to be cancelled
func (b *Batcher) Add(ctx context.Context, event Event) error {
	// Held across the send so Close can't retire the queue underneath an event that was already accepted
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

// noteDropped records a dropped event, rate limiting the log so a persistently full queue doesn't flood the
// logs with one line per request
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

	buf := make([]Event, 0, b.cfg.MaxSize)

	interval := time.Duration(b.cfg.MaxAge) * time.Second / batchTickDivisor
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	oldest := time.Time{}
	maxAge := time.Duration(b.cfg.MaxAge) * time.Second

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
				// Reset so a subsequent partial batch ages from when its own first event arrives, not
				// from the batch that was just written
				oldest = time.Now()
			}
		case <-ticker.C:
			if len(buf) > 0 && time.Since(oldest) >= maxAge {
				b.doFlush(buf)
				buf = buf[:0]
				oldest = time.Now()
			}
		case <-b.done:
			// Drain whatever is still queued before exiting so Close doesn't lose events
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

func (b *Batcher) doFlush(events []Event) {
	if len(events) == 0 {
		return
	}

	// The request context that produced these events is long gone so flushes run against a fresh background
	// context instead of one that may already be cancelled
	ctx := pkg.BackgroundContext()

	// Hand the flush its own copy, buf is reused for the next batch as soon as this returns
	batch := make([]Event, len(events))
	copy(batch, events)

	err := b.flush(ctx, batch)

	if err != nil {
		b.errorCounter.Add(ctx, int64(len(batch)))
		slog.WarnContext(ctx, fmt.Sprintf("Analytics module %v failed to write a batch of %v events: %v", b.id, len(batch), err))
		return
	}

	b.recordCounter.Add(ctx, int64(len(batch)))
}

// Close stops accepting events, drains the queue and performs a final flush. Returns once all workers have
// finished or the context is cancelled, whichever comes first
func (b *Batcher) Close(ctx context.Context) error {
	// Sealing takes the write lock, which waits for every in-flight Add to finish its send so the drain
	// sees a complete queue. Under OnFullBlock a producer can be parked on a full queue, so this races the
	// context; otherwise a hung flush would make Close ignore its deadline
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
		return fmt.Errorf("analytics module %v did not finish flushing before shutdown deadline: %w", b.id, ctx.Err())
	}
}
