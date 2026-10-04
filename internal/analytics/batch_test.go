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
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mutex   sync.Mutex
	batches [][]analytics.Event
	// When set, flush blocks until closed so a test can hold up the workers
	gate chan struct{}
	err  error
}

func (r *recorder) flush(_ context.Context, events []analytics.Event) error {
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

	// Explicit values survive
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
	// Long enough that only the size trigger can fire
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	for range 3 {
		require.NoError(t, b.Add(ctx, analytics.Event{LayerID: "l"}))
	}

	require.Eventually(t, func() bool { return rec.batchCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, 3, rec.count())

	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_FlushesOnAge(t *testing.T) {
	rec := &recorder{}

	cfg := testBatchConfig()
	// Far above what the test enqueues so only the age trigger can fire
	cfg.MaxSize = 1000
	cfg.MaxAge = 1

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, b.Add(ctx, analytics.Event{LayerID: "l"}))

	require.Eventually(t, func() bool { return rec.count() == 1 }, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_CloseFlushesPartialBatch(t *testing.T) {
	rec := &recorder{}

	cfg := testBatchConfig()
	// Only Close can produce a flush
	cfg.MaxSize = 1000
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, rec.flush)
	require.NoError(t, err)

	ctx := context.Background()

	for range 4 {
		require.NoError(t, b.Add(ctx, analytics.Event{LayerID: "l"}))
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

	// The worker parks inside flush so the queue backs up and stays full
	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 100 {
			// Must never block even though nothing drains the queue
			_ = b.Add(ctx, analytics.Event{LayerID: "l"})
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

	// Under backpressure Add should report the timeout rather than silently drop
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	var lastErr error

	for range 10 {
		lastErr = b.Add(ctx, analytics.Event{LayerID: "l"})
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

	require.NoError(t, b.Add(context.Background(), analytics.Event{LayerID: "l"}))

	// The worker is stuck in flush so Close must give up at its deadline
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

	// A failing destination must not fail Add since the tile was still served
	require.NoError(t, b.Add(ctx, analytics.Event{LayerID: "l"}))

	require.Eventually(t, func() bool { return rec.batchCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_FlushPanicIsContained(t *testing.T) {
	rec := &recorder{}
	panicker := func(ctx context.Context, events []analytics.Event) error {
		if rec.batchCount() == 0 {
			rec.mutex.Lock()
			rec.batches = append(rec.batches, nil)
			rec.mutex.Unlock()

			panic("simulated flush panic")
		}

		return rec.flush(ctx, events)
	}

	cfg := testBatchConfig()
	cfg.MaxSize = 1
	cfg.MaxAge = 600

	b, err := NewBatcher("test", cfg, panicker)
	require.NoError(t, err)

	ctx := context.Background()

	// The first flush panics and the worker must keep going
	require.NoError(t, b.Add(ctx, analytics.Event{LayerID: "first"}))
	require.Eventually(t, func() bool { return rec.batchCount() == 1 }, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, b.Add(ctx, analytics.Event{LayerID: "second"}))
	require.Eventually(t, func() bool { return rec.batchCount() == 2 }, 5*time.Second, 10*time.Millisecond)

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
				_ = b.Add(ctx, analytics.Event{LayerID: "l"})
			}
		}()
	}

	wg.Wait()

	require.NoError(t, b.Close(ctx))

	// Blocking mode plus the drain on Close means nothing is lost
	assert.Equal(t, goroutines*perGoroutine, rec.count())
}

func Test_Batcher_AddAfterCloseDoesNotPanic(t *testing.T) {
	rec := &recorder{}

	b, err := NewBatcher("test", testBatchConfig(), rec.flush)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, b.Close(ctx))

	// A reload can race a request so Add must error instead of panicking on a closed channel
	require.Error(t, b.Add(ctx, analytics.Event{LayerID: "l"}))

	// Idempotent
	require.NoError(t, b.Close(ctx))
}

func Test_Batcher_ConcurrentAddRacingCloseLosesNothing(t *testing.T) {
	const adders = 50

	for range 50 {
		rec := &recorder{}

		cfg := testBatchConfig()
		// Only the drain on Close can flush, so every event is accounted for
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

				if b.Add(ctx, analytics.Event{LayerID: "l"}) == nil {
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

		// Accepted events must be written or counted as dropped. Anything else is invisibly stranded
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
		go func() { _ = b.Add(context.Background(), analytics.Event{LayerID: "l"}) }()
	}

	// Let producers pile up against the full queue while the gated flush holds the worker
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Must give up at its deadline instead of waiting on producers it can't drain
	require.Error(t, b.Close(ctx))
}
