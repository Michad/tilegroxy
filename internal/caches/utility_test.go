// Copyright 2024 Michael Davis
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

package caches

import (
	"context"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostAndPortToString(t *testing.T) {
	hp := HostAndPort{"127.0.0.1", uint16(1234)}
	assert.Equal(t, "127.0.0.1:1234", hp.String())
}

func TestHostAndPortToStringArr(t *testing.T) {
	hp := HostAndPort{"127.0.0.1", uint16(1234)}
	hp2 := HostAndPort{"10.0.0.1", uint16(5678)}

	assert.Equal(t, []string{"127.0.0.1:1234", "10.0.0.1:5678"}, HostAndPortArrayToStringArray([]HostAndPort{hp, hp2}))
}

/*** safeLayerName / safeMemcachedKey ***/

func TestSafeLayerName_OrdinaryNamesPassThroughUnchanged(t *testing.T) {
	for _, name := range []string{"osm", "my-layer", "layer.v2", "Layer123", "a-b.c"} {
		assert.Equal(t, name, safeLayerName(name), "ordinary layer name %q should be unchanged", name)
	}
}

func TestSafeLayerName_PathTraversalIsNeutralized(t *testing.T) {
	assert.Equal(t, "____escaped", safeLayerName("../../escaped"))
	assert.NotContains(t, safeLayerName("../../escaped"), "..")
	assert.NotContains(t, safeLayerName("../../escaped"), "/")
}

func TestSafeLayerName_DotDotAlone(t *testing.T) {
	assert.Equal(t, "_", safeLayerName(".."))
}

func TestSafeLayerName_DotAlone(t *testing.T) {
	assert.Equal(t, "_", safeLayerName("."))
}

func TestSafeLayerName_RunsOfDotsAreNeutralized(t *testing.T) {
	assert.Equal(t, "_", safeLayerName("..."))
	assert.Equal(t, "_", safeLayerName("...."))
	assert.Equal(t, "a_b", safeLayerName("a..b"))
}

func TestSafeLayerName_SlashSeparatedName(t *testing.T) {
	assert.Equal(t, "a_b", safeLayerName("a/b"))
}

func TestSafeLayerName_NonASCIIIsReplaced(t *testing.T) {
	assert.Equal(t, "caf_", safeLayerName("café"))
	assert.Equal(t, "___", safeLayerName("日本語"))
}

func TestSafeLayerName_WhitespaceAndControlCharsAreReplaced(t *testing.T) {
	assert.Equal(t, "a_b", safeLayerName("a b"))
	assert.Equal(t, "a_b", safeLayerName("a\tb"))
	assert.Equal(t, "a_b", safeLayerName("a\nb"))
	assert.Equal(t, "a_b", safeLayerName("a\x00b"))
}

// The accepted collision tradeoff of sanitizing instead of hashing: layer names differing only in
// unsafe characters map to the same sanitized value.
func TestSafeLayerName_CollisionsAreExpectedForDistinctUnsafeNames(t *testing.T) {
	assert.Equal(t, safeLayerName("a/b"), safeLayerName("a b"))
}

func TestSafeMemcachedKey_ShortKeyIsUnchanged(t *testing.T) {
	key := safeMemcachedKey("prefix_", "osm/20/1/1")
	assert.Equal(t, "prefix_osm/20/1/1", key)
}

func TestSafeMemcachedKey_LongKeyIsShortenedAndStaysWithinLimit(t *testing.T) {
	prefix := "p_"
	longBody := strings.Repeat("a", 400) + "/20/1/1"

	key := safeMemcachedKey(prefix, longBody)

	assert.LessOrEqual(t, len(key), memcachedMaxKeyLength)
	assert.Contains(t, key, "_", "expected hash suffix to be appended")
}

func TestSafeMemcachedKey_LongKeysWithDifferentBodiesStayDistinct(t *testing.T) {
	prefix := "p_"
	body1 := strings.Repeat("a", 400) + "/20/1/1"
	body2 := strings.Repeat("a", 400) + "/20/1/2"

	key1 := safeMemcachedKey(prefix, body1)
	key2 := safeMemcachedKey(prefix, body2)

	assert.LessOrEqual(t, len(key1), memcachedMaxKeyLength)
	assert.LessOrEqual(t, len(key2), memcachedMaxKeyLength)
	assert.NotEqual(t, key1, key2, "distinct long keys should not collide after truncation")
}

func TestSafeMemcachedKey_PrefixCountsTowardLimit(t *testing.T) {
	longPrefix := strings.Repeat("p", 245)
	key := safeMemcachedKey(longPrefix, "osm/20/1/1")

	assert.LessOrEqual(t, len(key), memcachedMaxKeyLength)
}

/*** Utility methods used in most other cache tests ***/

func extractHostAndPort(t *testing.T, endpoint string) HostAndPort {
	split := strings.Split(endpoint, ":")
	port, err := strconv.Atoi(split[1])
	require.NoError(t, err)

	return HostAndPort{Host: split[0], Port: uint16(port)}
}

func makeReq(seed int) pkg.TileRequest {
	z := 20
	x := seed
	y := seed
	return pkg.TileRequest{LayerName: "test", Z: z, X: x, Y: y}
}

func makeImg(seed int) pkg.Image {
	return pkg.Image{Content: make([]byte, seed)}
}

func validateSaveAndLookup(t *testing.T, c cache.Cache) {
	tile := makeReq(rand.Intn(10000))
	img := makeImg(rand.Intn(100))

	err := c.Save(context.Background(), tile, &img)
	require.NoError(t, err, "Cache save returned an error")

	validateLookup(t, c, tile, &img)
}

func validateLookup(t *testing.T, c cache.Cache, tile pkg.TileRequest, expected *pkg.Image) {
	img2, err := c.Lookup(context.Background(), tile)
	require.NoError(t, err, "Cache lookup returned an error")
	require.NotNil(t, img2, "Cache lookup didn't return an image")

	require.True(t, slices.Equal(expected.Content, img2.Content), "Result before and after cache don't match")
}

func validateNoLookup(t *testing.T, c cache.Cache, tile pkg.TileRequest) {
	img2, err := c.Lookup(context.Background(), tile)
	require.NoError(t, err, "Cache lookup returned an error")
	require.Nil(t, img2, "Cache lookup returned a result when it shouldn't")
}
