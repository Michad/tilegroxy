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

package configload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	_ "github.com/spf13/viper/remote"
)

type ConfigWithID struct {
	ID     string
	Config map[string]interface{}
}

var CustomLogLevel = map[string]slog.Level{
	"trace":  config.LevelTrace,
	"absurd": config.LevelAbsurd,
}

// Covers fields entity construction doesn't touch, so `config check` can't report "Valid" for a config that breaks when served
func Validate(c config.Config) error {
	var errs []error

	switch c.Error.Mode {
	case config.ModeErrorPlainText, config.ModeErrorNoError, config.ModeErrorImage, config.ModeErrorImageHeader:
	default:
		errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "error.mode", c.Error.Mode))
	}

	if _, ok := CustomLogLevel[strings.ToLower(c.Logging.Main.Level)]; !ok {
		var level slog.Level
		if err := level.UnmarshalText([]byte(c.Logging.Main.Level)); err != nil {
			errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "logging.main.level", c.Logging.Main.Level))
		}
	}

	if c.Logging.Audit.Enabled {
		if !c.Logging.Audit.Console && c.Logging.Audit.Path == "" {
			errs = append(errs, fmt.Errorf(c.Error.Messages.OneOfRequired, []string{"logging.audit.console", "logging.audit.path"}))
		}
	}

	switch c.Logging.Main.Format {
	case config.MainFormatPlain, config.MainFormatJSON:
	default:
		errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "logging.main.format", c.Logging.Main.Format))
	}

	switch c.Logging.Access.Format {
	case config.AccessFormatCommon, config.AccessFormatCombined:
	default:
		errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "logging.access.format", c.Logging.Access.Format))
	}

	switch c.Logging.Audit.Format {
	case config.AuditFormatPlain, config.AuditFormatJSON:
	default:
		errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "logging.audit.format", c.Logging.Audit.Format))
	}

	if c.Server.Timeout == 0 {
		errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "server.timeout", "0"))
	}

	if c.Server.DrainDelay >= EffectiveShutdownTimeout(c.Server) {
		errs = append(errs, fmt.Errorf(c.Error.Messages.InvalidParam, "server.draindelay", strconv.FormatUint(uint64(c.Server.DrainDelay), 10)))
	}

	for i, l := range c.Layers {
		errs = validateLayer(c.Error.Messages, l, errs, i)
	}

	return errors.Join(errs...)
}

// Longitude, latitude, then an optional zoom
const (
	minCenterLen = 2
	maxCenterLen = 3
)

func validateLayer(messages config.ErrorMessages, l config.LayerConfig, errs []error, i int) []error {
	if l.MinZoom != nil && l.MaxZoom != nil && *l.MinZoom > *l.MaxZoom {
		errs = append(errs, fmt.Errorf(messages.InvalidParam, fmt.Sprintf("layers[%d].maxzoom", i), strconv.Itoa(*l.MaxZoom)))
	}

	if l.Bounds != (config.BoundsConfig{}) && (l.Bounds.South > l.Bounds.North || l.Bounds.West > l.Bounds.East) {
		errs = append(errs, fmt.Errorf(messages.InvalidParam, fmt.Sprintf("layers[%d].bounds", i), fmt.Sprintf("%+v", l.Bounds)))
	}

	if l.Center != nil {
		if len(l.Center) < minCenterLen || len(l.Center) > maxCenterLen {
			errs = append(errs, fmt.Errorf(messages.RangeError, fmt.Sprintf("layers[%d].center.size", i), minCenterLen, maxCenterLen))
		} else {
			if len(l.Center) == maxCenterLen {
				centerZoom := l.Center[2]
				effMaxZoom := 21
				if l.MaxZoom != nil && *l.MaxZoom < effMaxZoom {
					effMaxZoom = *l.MaxZoom
				}

				effMinZoom := 0
				if l.MinZoom != nil && *l.MinZoom > 0 {
					effMinZoom = *l.MinZoom
				}

				if centerZoom < float64(effMinZoom) || centerZoom > float64(effMaxZoom) {
					errs = append(errs, fmt.Errorf(messages.RangeError, fmt.Sprintf("layers[%d].center.zoom", i), effMinZoom, effMaxZoom))
				}
			}

			if l.Bounds != (config.BoundsConfig{}) {
				if l.Center[0] > l.Bounds.East || l.Center[0] < l.Bounds.West {
					errs = append(errs, fmt.Errorf(messages.RangeError, fmt.Sprintf("layers[%d].center[0]", i), l.Bounds.West, l.Bounds.East))
				}
				if l.Center[1] > l.Bounds.North || l.Center[1] < l.Bounds.South {
					errs = append(errs, fmt.Errorf(messages.RangeError, fmt.Sprintf("layers[%d].center[1]", i), l.Bounds.South, l.Bounds.North))
				}
			}
		}
	}
	return errs
}

// Normalizes the top-level cache config into a list with IDs
func NormalizeCaches(raw interface{}, errorMessages config.ErrorMessages) ([]ConfigWithID, error) {
	switch typed := raw.(type) {
	case nil:
		return []ConfigWithID{{ID: "none", Config: map[string]interface{}{"name": "none"}}}, nil
	case map[string]interface{}:
		id, ok := typed["id"].(string)
		if !ok || id == "" {
			id, ok = typed["name"].(string)

			if !ok || id == "" {
				return nil, fmt.Errorf(errorMessages.ParamRequired, "cache.name")
			}
		}

		return []ConfigWithID{{ID: id, Config: typed}}, nil
	}

	entries, err := toCacheEntryList(raw, errorMessages)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return []ConfigWithID{{ID: "none", Config: map[string]interface{}{"name": "none"}}}, nil
	}

	result := make([]ConfigWithID, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	for i, entry := range entries {
		id, _ := entry["id"].(string)
		if id == "" {
			// The docs let id default to name, which is unambiguous until two caches share a kind
			id, _ = entry["name"].(string)
		}

		if id == "" {
			return nil, fmt.Errorf(errorMessages.ParamRequired, fmt.Sprintf("cache[%d].id", i))
		}

		if seen[id] {
			return nil, fmt.Errorf(errorMessages.MustBeUnique, fmt.Sprintf("cache[%d].id", i), id)
		}

		seen[id] = true
		result = append(result, ConfigWithID{ID: id, Config: entry})
	}

	return result, nil
}

// Coerces the array forms a YAML or JSON decoder can produce
func toCacheEntryList(raw interface{}, errorMessages config.ErrorMessages) ([]map[string]interface{}, error) {
	switch typed := raw.(type) {
	case []map[string]interface{}:
		return typed, nil
	case []interface{}:
		entries := make([]map[string]interface{}, 0, len(typed))

		for i, rawEntry := range typed {
			entry, ok := rawEntry.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf(errorMessages.InvalidParam, fmt.Sprintf("cache[%d]", i), fmt.Sprintf("%#v", rawEntry))
			}

			entries = append(entries, entry)
		}

		return entries, nil
	}

	return nil, fmt.Errorf(errorMessages.InvalidParam, "cache", fmt.Sprintf("%#v", raw))
}

// Errors on unknown keys so a typo can't silently revert a security control. "id" stays since datastore declares it
func DecodeEntityConfig(rawConfig map[string]interface{}, out any) error {
	stripped := make(map[string]interface{}, len(rawConfig))
	for k, v := range rawConfig {
		if strings.EqualFold(k, "name") {
			continue
		}
		stripped[k] = v
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused: true,
		DecodeHook:  mapstructure.TextUnmarshallerHookFunc(),
		Result:      out,
	})
	if err != nil {
		return err
	}

	return decoder.Decode(stripped)
}

func initViper() *viper.Viper {
	var viper = viper.NewWithOptions(viper.KeyDelimiter("_"))
	viper.AutomaticEnv()
	registerDefaults(viper)
	return viper
}

// AutomaticEnv only reads env vars for keys viper already knows, which would otherwise be only those in the config file
func registerDefaults(v *viper.Viper) {
	b, err := json.Marshal(config.DefaultConfig())
	if err != nil {
		// DefaultConfig() is static, so a failure here is a programming error
		panic(err)
	}

	var asMap map[string]interface{}
	if err := json.Unmarshal(b, &asMap); err != nil {
		panic(err)
	}

	flattenDefaults(v, "", asMap)
}

func flattenDefaults(v *viper.Viper, prefix string, m map[string]interface{}) {
	for k, val := range m {
		key := k
		if prefix != "" {
			key = prefix + "_" + k
		}

		if nested, ok := val.(map[string]interface{}); ok {
			flattenDefaults(v, key, nested)
		} else {
			v.SetDefault(key, val)
		}
	}
}

func unmarshal(viper *viper.Viper) (config.Config, error) {
	c := config.DefaultConfig()

	// Viper silently merges a list of maps into one map. Analytics used this shape during development, so reject it explicitly
	if _, ok := viper.Get("analytics").([]interface{}); ok {
		return c, errors.New("analytics must be a single entry, not a list. Remove the leading '- ' and unindent the parameters beneath it")
	}

	// Same merging problem, but lists are valid for cache, so take the raw value before Unmarshal flattens it
	rawCaches, cacheIsList := viper.Get("cache").([]interface{})

	err := viper.Unmarshal(&c, func(dc *mapstructure.DecoderConfig) {
		dc.ErrorUnused = true
	})
	if err != nil {
		return c, err
	}

	if cacheIsList {
		c.Cache = rawCaches
	}

	return c, nil
}

func LoadConfig(raw string) (config.Config, error) {
	viper := initViper()

	if strings.Index(strings.TrimSpace(raw), "{") == 0 {
		viper.SetConfigType("json")
	} else {
		viper.SetConfigType("yaml")
	}

	err := viper.ReadConfig(bytes.NewBufferString(raw))
	if err != nil {
		return config.Config{}, err
	}

	return unmarshal(viper)
}

func LoadAndWatchConfigFromFile(filename string, onReload func(config.Config, error)) (config.Config, error) {
	viper := initViper()

	viper.SetConfigFile(filename)

	err := viper.ReadInConfig()

	if err != nil {
		return config.Config{}, err
	}

	if onReload != nil {
		configFile, err := filepath.Abs(filename)
		if err != nil {
			return config.Config{}, err
		}

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return config.Config{}, err
		}

		if err := watcher.Add(filepath.Dir(configFile)); err != nil {
			_ = watcher.Close()
			return config.Config{}, err
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					onReload(config.Config{}, fmt.Errorf("config watcher panic: %v\n%s", r, debug.Stack()))
				}
			}()

			watchConfigFile(filename, configFile, watcher, onReload)
		}()
	}

	return unmarshal(viper)
}

func watchConfigFile(filename, configFile string, watcher *fsnotify.Watcher, onReload func(config.Config, error)) {
	configDir := filepath.Dir(configFile)

	defer watcher.Close()

	var lastConfigLoad time.Time

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if filepath.Clean(event.Name) != configFile {
				continue
			}

			// The file's watch descriptor dies on remove/rename. Re-adding ensures a recreate is noticed and is harmless otherwise
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				_ = watcher.Add(configDir)
				continue
			}

			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}

			// Avoid duplicate file change events https://github.com/spf13/viper/issues/609
			if time.Since(lastConfigLoad) < time.Second {
				continue
			}
			lastConfigLoad = time.Now()

			// Separate goroutine so the delay below doesn't interfere with the dedupe above
			go func() {
				defer func() {
					if r := recover(); r != nil {
						onReload(config.Config{}, fmt.Errorf("config reload panic: %v\n%s", r, debug.Stack()))
					}
				}()

				// fsnotify can fire before the write finishes. May need exponential backoff someday https://github.com/spf13/viper/issues/1085
				time.Sleep(time.Second)

				reloaded := initViper()
				reloaded.SetConfigFile(filename)
				err := reloaded.ReadInConfig()

				if err != nil {
					onReload(config.Config{}, err)
				} else {
					onReload(unmarshal(reloaded))
				}
			}()
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func LoadConfigFromFile(filename string) (config.Config, error) {
	return LoadAndWatchConfigFromFile(filename, nil)
}

func LoadConfigFromRemote(provider, endpoint, path, format string) (config.Config, error) {
	viper := initViper()

	viper.SetConfigType(format)
	err := viper.AddRemoteProvider(provider, endpoint, path)

	if err != nil {
		return config.Config{}, err
	}

	err = viper.ReadRemoteConfig()

	if err != nil {
		return config.Config{}, err
	}

	return unmarshal(viper)
}
