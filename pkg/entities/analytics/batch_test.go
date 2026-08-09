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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder collects the batches a Batcher flushes so tests can assert on both grouping and content.
type recorder struct {
	mutex   sync.Mutex
	batches [][]Event
	// When set, flush blocks until this channel is closed, letting a test hold up the workers.
	gate chan struct{}
	err  error
}

func (r *recorder) flush(_ context.Context, events []Event) error {
	if r.gate != nil {
		<-r.gate
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.batches = append(r.batches, events)

	return r.err
}

func (r *recorder) count() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	total := 0
	for _, b := range r.batches {
		total += len(b)
	}

	return total
}

func (r *recorder) batchCount() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return len(r.batches)
}

func testBatchConfig() BatchConfig {
	cfg, _ := ApplyBatchDefaults(BatchConfig{}, config.DefaultConfig().Error.Messages)
	return cfg
}

func Test_ApplyBatchDefaults(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	cfg, err := ApplyBatchDefaults(BatchConfig{}, msgs)
	require.NoError(t, err)
	assert.Equal(t, uint(batchDefaultMaxSize), cfg.MaxSize)
	assert.Equal(t, uint(batchDefaultMaxAge), cfg.MaxAge)
	assert.Equal(t, uint(batchDefaultQueueSize), cfg.QueueSize)
	assert.Equal(t, uint(batchDefaultWorkers), cfg.Workers)
	assert.Equal(t, OnFullDrop, cfg.OnFull)

	// Explicit values survive.
	cfg, err = ApplyBatchDefaults(BatchConfig{MaxSize: 5, MaxAge: 1, QueueSize: 7, Workers: 3, OnFull: OnFullBlock}, msgs)
	require.NoError(t, err)
	assert.Equal(t, uint(5), cfg.MaxSize)
	assert.Equal(t, OnFullBlock, cfg.OnFull)

	_, err = ApplyBatchDefaults(BatchConfig{OnFull: "explode"}, msgs)
	require.Error(t, err)
}

func Test_Batcher_FlushesOnSize(t *testing.T) {
	rec := &recorder{}

	cfg := testBatchConfig()
	cfg.MaxSize = 3
	// Long enough that only the size trigger can fire during the test.
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	for range 3 {
		require.NoError(t, b.Add(ctx, Event{LayerID: "l"}))
	}

	require.Eventually(t, func() bool { return rec.batchCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 3, rec.count())

	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_FlushesOnAge(t *testing.T) {
	rec := &recorder{}

	cfg := testBatchConfig()
	// Far above what the test enqueues, so only the age trigger can fire.
	cfg.MaxSize = 1000
	cfg.MaxAge = 1

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, b.Add(ctx, Event{LayerID: "l"}))

	require.Eventually(t, func() bool { return rec.count() == 1 }, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_CloseFlushesPartialBatch(t *testing.T) {
	rec := &recorder{}

	cfg := testBatchConfig()
	// Neither trigger can fire on its own; only Close can produce a flush.
	cfg.MaxSize = 1000
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	for range 4 {
		require.NoError(t, b.Add(ctx, Event{LayerID: "l"}))
	}

	require.NoError(t, b.Close(ctx))

	assert.Equal(t, 4, rec.count(), "Close should drain and flush queued events rather than discard them")
}

func Test_Batcher_DropsWhenFullWithoutBlocking(t *testing.T) {
	gate := make(chan struct{})
	rec := &recorder{gate: gate}

	cfg := testBatchConfig()
	cfg.MaxSize = 1
	cfg.MaxAge = 600
	cfg.QueueSize = 1
	cfg.Workers = 1
	cfg.OnFull = OnFullDrop

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	// The worker parks inside flush holding the gate, so the queue backs up and stays full.
	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 100 {
			// Must never block even though nothing is draining the queue.
			_ = b.Add(ctx, Event{LayerID: "l"})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(gate)
		t.Fatal("Add blocked despite onFull being set to drop")
	}

	assert.Positive(t, b.dropped.Load(), "events should have been counted as dropped")

	close(gate)
	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_BlocksWhenFullUnderOnFullBlock(t *testing.T) {
	gate := make(chan struct{})
	rec := &recorder{gate: gate}

	cfg := testBatchConfig()
	cfg.MaxSize = 1
	cfg.MaxAge = 600
	cfg.QueueSize = 1
	cfg.Workers = 1
	cfg.OnFull = OnFullBlock

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	// A context that expires quickly stands in for a caller unwilling to wait forever; under
	// backpressure Add should report that rather than silently dropping.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	var lastErr error

	for range 10 {
		lastErr = b.Add(ctx, Event{LayerID: "l"})
		if lastErr != nil {
			break
		}
	}

	require.Error(t, lastErr, "Add should surface backpressure rather than drop under onFull: block")
	assert.Zero(t, b.dropped.Load(), "blocking mode must not drop events")

	close(gate)
	require.NoError(t, b.Close(context.Background()))
}

func Test_Batcher_CloseRespectsDeadline(t *testing.T) {
	gate := make(chan struct{})
	rec := &recorder{gate: gate}

	cfg := testBatchConfig()
	cfg.MaxSize = 1
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	require.NoError(t, b.Add(context.Background(), Event{LayerID: "l"}))

	// The worker is stuck in flush, so Close cannot complete and must give up at its deadline
	// rather than hanging shutdown forever.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = b.Close(ctx)
	require.Error(t, err)

	close(gate)
}

func Test_Batcher_FlushErrorIsContained(t *testing.T) {
	rec := &recorder{err: errors.New("destination is down")}

	cfg := testBatchConfig()
	cfg.MaxSize = 1
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	// A failing destination must not make Add fail: the tile was still served.
	require.NoError(t, b.Add(ctx, Event{LayerID: "l"}))

	require.Eventually(t, func() bool { return rec.batchCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_ConcurrentAdd(t *testing.T) {
	rec := &recorder{}

	cfg := testBatchConfig()
	cfg.MaxSize = 10
	cfg.MaxAge = 600
	cfg.QueueSize = 10000
	cfg.Workers = 4
	cfg.OnFull = OnFullBlock

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	const goroutines = 8
	const perGoroutine = 100

	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range perGoroutine {
				_ = b.Add(ctx, Event{LayerID: "l"})
			}
		}()
	}

	wg.Wait()

	require.NoError(t, b.Close(ctx))

	// Every event must be accounted for: blocking mode plus a drain on Close means none are lost.
	assert.Equal(t, goroutines*perGoroutine, rec.count())
}

func Test_Batcher_AddAfterCloseDoesNotPanic(t *testing.T) {
	rec := &recorder{}

	b, err := NewBatcher("test", testBatchConfig(), rec.flush)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, b.Close(ctx))

	// A reload could race a request; Add must report rather than panic on a closed channel.
	require.Error(t, b.Add(ctx, Event{LayerID: "l"}))

	// Close is idempotent.
	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_ConcurrentAddRacingCloseLosesNothing(t *testing.T) {
	const adders = 50

	for range 50 {
		rec := &recorder{}

		cfg := testBatchConfig()
		// Neither trigger can fire on its own so every event is accounted for by the drain on Close.
		cfg.MaxSize = 1000
		cfg.MaxAge = 600
		cfg.Workers = 2

		b, err := NewBatcher("test", cfg, rec.flush)
		require.NoError(t, err)

		ctx := context.Background()

		var accepted atomic.Int64
		var wg sync.WaitGroup

		start := make(chan struct{})

		for range adders {
			wg.Add(1)

			go func() {
				defer wg.Done()
				<-start

				if b.Add(ctx, Event{LayerID: "l"}) == nil {
					accepted.Add(1)
				}
			}()
		}

		wg.Add(1)

		go func() {
			defer wg.Done()
			<-start
			_ = b.Close(ctx)
		}()

		close(start)
		wg.Wait()

		require.NoError(t, b.Close(ctx))

		// An event that Add accepted must end up either written or counted as dropped. Anything else is
		// stranded in the queue where no metric would ever reveal it.
		assert.Equal(t, accepted.Load(), int64(rec.count())+int64(b.dropped.Load()),
			"every accepted event should be flushed or counted as dropped")
	}
}

func Test_Batcher_CloseHonorsDeadlineWhileProducersBlock(t *testing.T) {
	gate := make(chan struct{})
	rec := &recorder{gate: gate}

	defer close(gate)

	cfg := testBatchConfig()
	cfg.MaxSize = 2
	cfg.MaxAge = 600
	cfg.QueueSize = 1
	cfg.Workers = 1
	cfg.OnFull = OnFullBlock

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	for range 10 {
		go func() { _ = b.Add(context.Background(), Event{LayerID: "l"}) }()
	}

	// Let the producers pile up against the full queue while the gated flush holds the worker.
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Close must give up on its deadline instead of waiting forever on producers it can't drain.
	require.Error(t, b.Close(ctx))
}
