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

package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/redis/go-redis/v9"
)

const redisDefaultKeyPrefix = "ratelimit:"

type RedisConfig struct {
	Datastore string // ID of a redis datastore
	KeyPrefix string // Prepended to every key. Defaults to ratelimit:
}

type Redis struct {
	client redis.UniversalClient
	prefix string
}

func init() {
	ratelimit.RegisterRateLimiter(RedisRegistration{})
}

type RedisRegistration struct{}

func (RedisRegistration) InitializeConfig() any {
	return RedisConfig{KeyPrefix: redisDefaultKeyPrefix}
}

func (RedisRegistration) Name() string {
	return "redis"
}

func (RedisRegistration) Initialize(cfgAny any, deps ratelimit.RateLimiterDeps) (ratelimit.RateLimiter, error) {
	cfg := cfgAny.(RedisConfig)

	if cfg.Datastore == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "ratelimit.redis.datastore")
	}

	var client redis.UniversalClient

	var ok bool

	if deps.Datastores != nil {
		var ds datastore.DatastoreWrapper

		if ds, ok = deps.Datastores.Get(cfg.Datastore); ok {
			client, ok = ds.Native().(redis.UniversalClient)
		}
	}

	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "ratelimit.redis.datastore", cfg.Datastore)
	}

	return &Redis{client: client, prefix: cfg.KeyPrefix}, nil
}

func (r *Redis) key(c ratelimit.RateLimitCounter) string {
	return r.prefix + c.Key + ":" + strconv.FormatInt(c.Window.Unix(), 10)
}

// Pipelined rather than MGET since counters can live on different cluster nodes
func (r *Redis) Get(ctx context.Context, counters []ratelimit.RateLimitCounter) ([]int64, error) {
	cmds := make([]*redis.StringCmd, len(counters))

	_, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for i, c := range counters {
			cmds[i] = pipe.Get(ctx, r.key(c))
		}

		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	result := make([]int64, len(counters))

	for i, cmd := range cmds {
		n, err := cmd.Int64()

		switch {
		case errors.Is(err, redis.Nil):
		case err != nil:
			return nil, err
		default:
			result[i] = n
		}
	}

	return result, nil
}

// The TTL is refreshed on every increment, each time from the server's own clock
func (r *Redis) Increment(ctx context.Context, counters []ratelimit.RateLimitCounter) error {
	_, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, c := range counters {
			key := r.key(c)
			pipe.Incr(ctx, key)
			pipe.PExpire(ctx, key, c.TTL)
		}

		return nil
	})

	return err
}

func (r *Redis) Reset(ctx context.Context, counters []ratelimit.RateLimitCounter) error {
	_, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, c := range counters {
			pipe.Del(ctx, r.key(c))
		}

		return nil
	})

	return err
}

func (r *Redis) Now(ctx context.Context) (time.Time, error) {
	return r.client.Time(ctx).Result()
}
