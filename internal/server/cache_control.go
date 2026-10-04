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

package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

const cacheControlHeader = "Cache-Control"
const noStoreDirective = "no-store"

// path is the dotted prefix of the block so the message names the right place
func ValidateCacheControl(cfg config.CacheControlConfig, path string, errorMessages config.ErrorMessages) error {
	if !configload.CacheControlEnabled(cfg) {
		return nil
	}

	if cfg.Visibility != "" && cfg.Visibility != config.CacheVisibilityPublic && cfg.Visibility != config.CacheVisibilityPrivate {
		return fmt.Errorf(errorMessages.EnumError, path+".visibility", cfg.Visibility, []string{config.CacheVisibilityPublic, config.CacheVisibilityPrivate})
	}

	for _, extra := range cfg.Extra {
		if strings.Contains(extra, ",") {
			return fmt.Errorf(errorMessages.InvalidParam, path+".extra", extra)
		}
	}

	if cfg.NoStore == nil || !*cfg.NoStore {
		return nil
	}

	for _, conflict := range checkNoStoreConflicts(cfg) {
		return fmt.Errorf(errorMessages.ParamsMutuallyExclusive, path+"."+conflict, path+".nostore")
	}

	return nil
}

// no-store keeps nothing, so any lifetime directive in the same block contradicts it
func checkNoStoreConflicts(cfg config.CacheControlConfig) []string {
	var conflicts []string

	if cfg.MaxAge != nil {
		conflicts = append(conflicts, "maxage")
	}

	if cfg.SharedMaxAge != nil {
		conflicts = append(conflicts, "sharedmaxage")
	}

	if cfg.StaleWhileRevalidate != nil {
		conflicts = append(conflicts, "stalewhilerevalidate")
	}

	if cfg.Visibility != "" {
		conflicts = append(conflicts, "visibility")
	}

	if len(cfg.Extra) > 0 {
		conflicts = append(conflicts, "extra")
	}

	return conflicts
}

func mergeCacheControl(serverCfg config.CacheControlConfig, l *layers.Layer) config.CacheControlConfig {
	if l == nil || l.Config.CacheControl == nil {
		return serverCfg
	}

	merged := *l.Config.CacheControl
	configload.MergeCacheControlDefaults(&merged, serverCfg)

	return merged
}

func generateCacheControlValue(cfg config.CacheControlConfig, l *layers.Layer, img *pkg.Image, now time.Time) string {
	if !configload.CacheControlEnabled(cfg) {
		return ""
	}

	var facts layers.CacheControlFacts
	if configload.CacheControlAutoEnabled(cfg) && l != nil {
		facts = l.CacheControl
	}

	if (cfg.NoStore != nil && *cfg.NoStore) || (cfg.NoStore == nil && facts.Uncacheable) {
		return noStoreDirective
	}

	var directives []string

	if visibility := resolveVisibility(cfg, facts); visibility != "" {
		directives = append(directives, visibility)
	}

	if maxAge, ok := resolveRemainingTTL(cfg, facts, img, now); ok {
		directives = append(directives, "max-age="+strconv.FormatUint(uint64(maxAge), 10))
	}

	if cfg.SharedMaxAge != nil {
		directives = append(directives, "s-maxage="+strconv.FormatUint(uint64(*cfg.SharedMaxAge), 10))
	}

	if cfg.StaleWhileRevalidate != nil {
		directives = append(directives, "stale-while-revalidate="+strconv.FormatUint(uint64(*cfg.StaleWhileRevalidate), 10))
	}

	directives = append(directives, cfg.Extra...)

	return strings.Join(directives, ", ")
}

func resolveVisibility(cfg config.CacheControlConfig, facts layers.CacheControlFacts) string {
	if cfg.Visibility != "" {
		return cfg.Visibility
	}

	if !configload.CacheControlAutoEnabled(cfg) {
		return ""
	}

	if facts.PerIdentity {
		return config.CacheVisibilityPrivate
	}

	return config.CacheVisibilityPublic
}

func resolveRemainingTTL(cfg config.CacheControlConfig, facts layers.CacheControlFacts, img *pkg.Image, now time.Time) (uint, bool) {
	if cfg.MaxAge != nil {
		return *cfg.MaxAge, true
	}

	if !configload.CacheControlAutoEnabled(cfg) || facts.TTL <= 0 {
		return 0, false
	}

	remaining := facts.TTL

	// Zero for entries written around the TTL cache, which treats them as fresh, so they get the full TTL
	if img != nil && img.CreatedAt != 0 {
		remaining = facts.TTL - now.Sub(time.Unix(img.CreatedAt, 0))
	}

	if remaining < 0 {
		remaining = 0
	}

	return uint(remaining.Seconds()), true
}

func (s *generation) generateCacheControlForTile(ctx context.Context, tileReq pkg.TileRequest, img *pkg.Image) string {
	l := s.layerGroup().FindLayer(ctx, tileReq.LayerName)

	return generateCacheControlValue(mergeCacheControl(s.serverCfg.CacheControl, l), l, img, time.Now())
}

// Validates the server level and every layer
func validateAllCacheControl(cfg *config.Config) error {
	if err := ValidateCacheControl(cfg.Server.CacheControl, "server.cachecontrol", cfg.Error.Messages); err != nil {
		return err
	}

	for _, l := range cfg.Layers {
		if l.CacheControl == nil {
			continue
		}

		merged := *l.CacheControl
		configload.MergeCacheControlDefaults(&merged, cfg.Server.CacheControl)

		if err := ValidateCacheControl(merged, "layer."+l.ID+".cachecontrol", cfg.Error.Messages); err != nil {
			return err
		}
	}

	return nil
}
