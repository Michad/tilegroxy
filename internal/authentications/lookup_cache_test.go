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

package authentications

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLookupCache(t *testing.T) *lookupCache[string] {
	t.Helper()

	c, err := newLookupCache[string](100, 0)
	require.NoError(t, err)
	t.Cleanup(c.close)

	return c
}

// Holds a lookup open for key until the returned func is called
func blockLookup(t *testing.T, c *lookupCache[string], key string) func() {
	t.Helper()

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = c.get(context.Background(), key, func() (string, error) {
			close(started)
			<-release
			return "slow", nil
		})
	}()
	<-started

	return func() {
		close(release)
		<-done
	}
}

func Test_LookupCache_Nil(t *testing.T) {
	var c *lookupCache[string]
	calls := 0

	for range 2 {
		result, err := c.get(context.Background(), "k", func() (string, error) {
			calls++
			return "v", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "v", result)
	}

	assert.Equal(t, 2, calls)
	c.close()
}

func Test_LookupCache_CachesSuccessNotError(t *testing.T) {
	c := testLookupCache(t)
	calls := 0
	lookupErr := errors.New("down")

	_, err := c.get(context.Background(), "k", func() (string, error) {
		calls++
		return "", lookupErr
	})
	require.ErrorIs(t, err, lookupErr)

	for range 2 {
		result, err := c.get(context.Background(), "k", func() (string, error) {
			calls++
			return "v", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "v", result)
	}

	assert.Equal(t, 2, calls)
}

func Test_LookupCache_StaleEntryReloads(t *testing.T) {
	c := testLookupCache(t).withStaleCheck(func(v string) bool { return v == "stale" })

	_, err := c.get(context.Background(), "k", func() (string, error) { return "stale", nil })
	require.NoError(t, err)

	result, err := c.get(context.Background(), "k", func() (string, error) { return "fresh", nil })
	require.NoError(t, err)
	assert.Equal(t, "fresh", result)
}

func Test_LookupCache_SlowLookupDoesNotBlockOtherKeys(t *testing.T) {
	c := testLookupCache(t)

	_, err := c.get(context.Background(), "cached", func() (string, error) { return "v", nil })
	require.NoError(t, err)

	release := blockLookup(t, c, "slow")
	defer release()

	result, err := c.get(context.Background(), "cached", func() (string, error) {
		t.Fatal("cached key must not be looked up again")
		return "", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "v", result)

	result, err = c.get(context.Background(), "other", func() (string, error) { return "other", nil })
	require.NoError(t, err)
	assert.Equal(t, "other", result)
}

func Test_LookupCache_ConcurrentSameKeyShareLookup(t *testing.T) {
	c := testLookupCache(t)
	var calls atomic.Int32
	release := make(chan struct{})

	var wg sync.WaitGroup
	results := make([]string, 10)
	for i := range results {
		wg.Go(func() {
			results[i], _ = c.get(context.Background(), "k", func() (string, error) {
				calls.Add(1)
				<-release
				return "v", nil
			})
		})
	}

	// Gives every goroutine time to join the flight before it completes
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.Equal(t, int32(1), calls.Load())
	for _, r := range results {
		assert.Equal(t, "v", r)
	}
}

func Test_LookupCache_WaiterHonorsContext(t *testing.T) {
	c := testLookupCache(t)

	release := blockLookup(t, c, "k")
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.get(ctx, "k", func() (string, error) { return "unused", nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func Test_LookupCache_WaiterRetriesWhenLeaderCancelled(t *testing.T) {
	c := testLookupCache(t)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	started := make(chan struct{})

	go func() {
		_, _ = c.get(leaderCtx, "k", func() (string, error) {
			close(started)
			<-leaderCtx.Done()
			return "", leaderCtx.Err()
		})
	}()
	<-started

	resultCh := make(chan string)
	go func() {
		result, _ := c.get(context.Background(), "k", func() (string, error) { return "retried", nil })
		resultCh <- result
	}()

	time.Sleep(20 * time.Millisecond)
	cancelLeader()

	assert.Equal(t, "retried", <-resultCh)
}

func Test_LookupCache_WaiterRetriesWhenLeaderPanics(t *testing.T) {
	c := testLookupCache(t)
	started := make(chan struct{})
	release := make(chan struct{})

	go func() {
		defer func() { _ = recover() }()
		_, _ = c.get(context.Background(), "k", func() (string, error) {
			close(started)
			<-release
			panic("boom")
		})
	}()
	<-started

	resultCh := make(chan string)
	go func() {
		result, _ := c.get(context.Background(), "k", func() (string, error) { return "retried", nil })
		resultCh <- result
	}()

	time.Sleep(20 * time.Millisecond)
	close(release)

	assert.Equal(t, "retried", <-resultCh)
}

func Test_LookupCache_WaiterSharesLeaderError(t *testing.T) {
	c := testLookupCache(t)
	lookupErr := errors.New("rejected")
	started := make(chan struct{})
	release := make(chan struct{})

	go func() {
		_, _ = c.get(context.Background(), "k", func() (string, error) {
			close(started)
			<-release
			return "", lookupErr
		})
	}()
	<-started

	errCh := make(chan error)
	go func() {
		_, err := c.get(context.Background(), "k", func() (string, error) { return "unused", nil })
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	close(release)

	assert.ErrorIs(t, <-errCh, lookupErr)
}

func Test_LookupCache_HashesKeys(t *testing.T) {
	c := testLookupCache(t)
	rawKey := string(make([]byte, 1<<20))

	_, err := c.get(context.Background(), rawKey, func() (string, error) { return "v", nil })
	require.NoError(t, err)

	c.cache.Range(func(key string, _ string) bool {
		assert.Len(t, key, 32)
		return true
	})
}
