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

package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

// ttlEnvelopeVersion tags the gob payload TTLCache stores so a future format change can be
// detected the same way pkg.Image.Encode/DecodeImage already version their own encoding.
const ttlEnvelopeVersion = "v1"

// ttlEnvelope records when an entry was written so TTLCache can decide, purely on read, whether
// it's still fresh. WrittenAt is a Unix timestamp (seconds) rather than time.Time so gob output
// doesn't depend on time.Time's monotonic reading or location.
type ttlEnvelope struct {
	WrittenAt int64
	Image     pkg.Image
}

// encodeTTLEnvelope wraps img with the current time for storage in the backing cache.
func encodeTTLEnvelope(img *pkg.Image, now time.Time) (*pkg.Image, error) {
	b := bytes.Buffer{}
	e := gob.NewEncoder(&b)

	if err := e.Encode(ttlEnvelopeVersion); err != nil {
		return nil, err
	}
	if err := e.Encode(ttlEnvelope{WrittenAt: now.Unix(), Image: *img}); err != nil {
		return nil, err
	}

	return &pkg.Image{Content: b.Bytes(), ContentType: img.ContentType}, nil
}

// decodeTTLEnvelope reverses encodeTTLEnvelope. A payload that doesn't parse as an envelope is
// data written before TTL support existed, or by a cache backend TTLCache doesn't wrap. That's
// treated as a hit with no known write time rather than an error: the alternative (miss) would
// silently invalidate every entry in an existing cache the moment TTL is enabled, and the
// alternative (crash) would take down the request path over old data.
func decodeTTLEnvelope(img *pkg.Image) (envelope ttlEnvelope, ok bool) {
	d := gob.NewDecoder(bytes.NewReader(img.Content))

	var version string
	if err := d.Decode(&version); err != nil || version != ttlEnvelopeVersion {
		return ttlEnvelope{}, false
	}

	if err := d.Decode(&envelope); err != nil {
		return ttlEnvelope{}, false
	}

	return envelope, true
}

// TTLCache wraps any Cache to enforce a uniform expiration, emulating it for backends with no
// native notion of TTL. An expired entry is reported as a cache miss rather than deleted: the
// normal request path then regenerates and overwrites it, which is all a per-layer TTL needs.
type TTLCache struct {
	Cache Cache
	TTL   time.Duration
	// clock returns the current time. Overridden in tests, defaults to time.Now.
	clock func() time.Time
}

// NewTTLCache wraps inner so every entry expires ttl after it was written, regardless of whether
// inner has any native expiry of its own.
func NewTTLCache(inner Cache, ttl time.Duration) *TTLCache {
	return &TTLCache{Cache: inner, TTL: ttl, clock: time.Now}
}

func (c *TTLCache) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

func (c *TTLCache) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	stored, err := c.Cache.Lookup(ctx, t)
	if err != nil || stored == nil {
		return stored, err
	}

	envelope, ok := decodeTTLEnvelope(stored)
	if !ok {
		// Pre-existing entry written before TTL was enabled (or by a path that bypasses this
		// wrapper). Treat as fresh rather than as a miss or a crash.
		return stored, nil
	}

	if c.now().Sub(time.Unix(envelope.WrittenAt, 0)) > c.TTL {
		return nil, nil
	}

	img := envelope.Image
	return &img, nil
}

func (c *TTLCache) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	wrapped, err := encodeTTLEnvelope(img, c.now())
	if err != nil {
		return err
	}

	return c.Cache.Save(ctx, t, wrapped)
}

// Close forwards to the wrapped cache when it holds resources needing release, the same way
// CacheWrapper does. TTLCache can sit between CacheWrapper and the real backend, so it needs to
// pass Close through too or a wrapped Closer would become unreachable during shutdown.
func (c *TTLCache) Close(ctx context.Context) error {
	return lifecycle.CloseIfCloser(ctx, c.Cache)
}
