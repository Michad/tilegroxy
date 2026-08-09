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

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg/static"
	"github.com/fsnotify/fsnotify"
	viperMapstructure "github.com/go-viper/mapstructure/v2"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
	_ "github.com/spf13/viper/remote"
)

// Configuration for TLS (HTTPS) operation. If this is configured then TLS is enabled. This can operate either with a static certificate and keyfile via the filesystem or via ACME/Let's Encrypt
type EncryptionConfig struct {
	Domain      string // The domain name you're operating with (the domain end-users use). Required
	Cache       string // The path to a directory to cache certificates in if using let's encrypt. Defaults to ./certs
	Certificate string // The file path to get to the TLS certificate
	KeyFile     string // The file path to get to the keyfile
	HTTPPort    int    // The port used for non-encrypted traffic. Required if using Let's Encrypt for ACME challenge and needs to indirectly be 80 (that is, it could be 8080 if something else redirects 80 to 8080). Everything except .well-known will be redirected to the main port when set.
}

// Configuration for health checks
type HealthConfig struct {
	Enabled bool             // If set to false the port isn't bound to. Defaults false
	Port    int              // The port to serve health on. Defaults to 3000
	Host    string           // The host to bind to. Defaults to 0.0.0.0
	Checks  []map[string]any // An array defining the specific checks to perform.
}

type ServerConfig struct {
	Encrypt    *EncryptionConfig // Whether and how to use TLS. Defaults to none AKA no encryption.
	Health     HealthConfig      // Whether to enable health endpoints on a secondary port.
	BindHost   string            // IP address to bind HTTP server to
	Port       int               // Port to bind HTTP server to
	RootPath   string            // Root HTTP Path to apply to all endpoints. Defaults to /
	TilePath   string            // HTTP Path to serve tiles under (in addition to RootPath). Defaults to tiles which means /tiles/{layer}/{z}/{x}/{y}.
	DocsPath   string            // HTTP Path for accessing the documentation website. Defaults to docs
	Headers    map[string]string // Include these headers in all response from server
	Production bool              // Controls serving splash page, documentation, x-powered-by header. Defaults to false, set true to harden for prod
	Timeout    uint              // How long (in seconds) a request can be in flight before we cancel it and return an error
	Gzip       bool              // Whether to apply gzip compression. Not super helpful when just serving up raster images
}

type ClientConfig struct {
	UserAgent           string            // The user agent to include in outgoing http requests. Separate from Headers to avoid omitting this.
	MaxLength           int               // The maximum Content-Length to allow incoming responses. Default: 10 Megabytes
	UnknownLength       bool              // If true, allow responses that are missing a Content-Length header, this could lead to memory overruns. Default: false. Not inherited from the global client config by layers - see MergeDefaultsFrom
	ContentTypes        []string          // The content-types to allow servers to return. Anything else will be interpreted as an error
	StatusCodes         []int             // The status codes from the remote server to consider successful.  Defaults to just 200
	Headers             map[string]string // Include these headers in requests. Defaults to none
	Timeout             uint              // How long (in seconds) a request can be in flight before we cancel it and return an error
	RewriteContentTypes map[string]string // Replace ContentType's that match the key with the value. This is to handle servers returning a generic content type. Kicks in after the check that ContentType is in `ContentTypes`.

}

type TelemetryConfig struct {
	Enabled bool
}

// TODO: handle this better. Not foolproof in detecting default values and very manual. Probably need to do a mapstructure method for this
func (c *ClientConfig) MergeDefaultsFrom(o ClientConfig) {
	if c.UserAgent == "" {
		c.UserAgent = o.UserAgent
	}
	if c.MaxLength == 0 {
		c.MaxLength = o.MaxLength
	}
	// UnknownLength is deliberately NOT inherited. It's a bool whose zero value, its default, and
	// the value a layer would set to tighten the limit are all `false`, so "unset" and "explicitly
	// false" are indistinguishable here. A `if !c.UnknownLength { c.UnknownLength = o.UnknownLength }`
	// therefore only ever fires in the case you don't want: it overrides a layer that explicitly
	// set `unknownlength: false` to tighten a permissive global default, and can never be observed
	// doing anything else. Making the field a *bool would distinguish the two, but that ripples
	// through call sites outside this package; until that happens, not inheriting (which is what
	// this has always done) is the safe direction - a layer that wants the permissive behavior can
	// state it explicitly.
	if len(c.Headers) == 0 {
		c.Headers = o.Headers
	}
	if len(c.ContentTypes) == 0 {
		c.ContentTypes = o.ContentTypes
	}
	if len(c.StatusCodes) == 0 {
		c.StatusCodes = o.StatusCodes
	}
	if c.Timeout == 0 {
		c.Timeout = o.Timeout
	}
	if len(c.RewriteContentTypes) == 0 {
		c.RewriteContentTypes = o.RewriteContentTypes
	}
}

// Modes for error reporting
const (
	ModeErrorPlainText   = "text"         // Response will be text/plain with the error message in the body
	ModeErrorNoError     = "none"         // Response will not include any data but will return status code.
	ModeErrorImage       = "image"        // Response will return an image but not the error itself
	ModeErrorImageHeader = "image+header" // Response will return an image and include the error inside x-error-message
)

// This is a poor-man's i8n solution. It allows replacing the error messages our app generates in the main `serve` mode.
// It's questionable if anyone will ever want to make use of it, but it at least helps avoid magic strings and can be
// replaced with fully static constants later if it does turn out nobody ever sees value in it
type ErrorMessages struct {
	NotAuthorized           string
	ParamRequired           string
	InvalidParam            string
	RangeError              string
	ServerError             string
	ProviderError           string
	ParamsBothOrNeither     string
	ParamsMutuallyExclusive string
	OneOfRequired           string
	EnumError               string
	ScriptError             string
	Timeout                 string
	ParamRegex              string
}

// Default embedded image keys, mirrored from internal/images (which pkg/config can't import - it's
// an internal/ package, so importing it would make this exported struct's defaults depend on an
// unexportable package). internal/images.GetStaticImage recognizes these same literal strings.
const (
	defaultImageError        = "embedded:error.png"
	defaultImageTransparent  = "embedded:transparent.png"
	defaultImageUnauthorized = "embedded:unauthorized.png"
)

// Selects what image to return when various errors occur. These should either be an embedded:XXX value reflecting an image in `internal/images` or the path to an image in the runtime filesystem
type ErrorImages struct {
	OutOfBounds    string // A request for a zoom level or tile coordinate that's invalid for the requested layer
	Authentication string // Auth failed
	Provider       string // Provider specific errors
	Other          string // Catch-all for unexpected system errors
}

type ErrorConfig struct {
	Mode     string        // How errors should be returned.  See the consts above for options
	Messages ErrorMessages // Patterns to use for error messages in logs and responses. Not used for utility commands.
	Images   ErrorImages   // Only used if Mode is image or image+header
	AlwaysOK bool          // If set we always return 200 regardless of what happens
}

// Formats for outputting the access log
const (
	AccessFormatCommon   = "common"
	AccessFormatCombined = "combined"
)

type AccessConfig struct {
	Console bool   // If true, write access logs to standard out. Defaults to true
	Path    string // The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string // The format to output access logs in. Applies to both standard out and file out. Possible values: common, combined. Defaults to common
}

// Formats for outputting the main log
const (
	MainFormatPlain = "plain"
	MainFormatJSON  = "json"
)

const LevelTrace slog.Level = slog.LevelDebug - 5
const LevelAbsurd slog.Level = slog.LevelDebug - 10

var CustomLogLevel = map[string]slog.Level{
	"trace":  LevelTrace,
	"absurd": LevelAbsurd,
}

type MainConfig struct {
	Console bool     // If true, write access logs to standard out. Defaults to true
	Path    string   // The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string   // The format to output access logs in. Applies to both standard out and file out. Possible values: plain, json. Defaults to plain
	Level   string   // logging level. one of: debug, info, warn, error, trace, absurd
	Request string   // Can be "true", "false" or "auto". If false, don't include any extra attributes based on request parameters (excluding the ones requested below). If auto (default) it defaults true if format is json, false otherwise
	Headers []string // Headers to include in the logs. Useful for a transaction/request/trace/correlation ID or user identifiers
}

type LogConfig struct {
	Access AccessConfig
	Main   MainConfig
}

// Defines a layer to be served up by the application
type LayerConfig struct {
	ID             string            // A distinct identifier for this layer. If no pattern is defined this is used to match against the layer name. Also used
	Pattern        string            // A pattern to match against for layer names in incoming requests. Includes placeholders from which values can be extracted when matching. Not regular expressions, placeholders are simply wrapped in curly braces
	ParamValidator map[string]string // A mapping of regular expressions to use for each value extracted from the pattern. Keys must match the placeholders in pattern. This is external from the pattern itself to keep parsing the pattern simple and less error prone. If a key of "*" is defined it applies to all placeholders
	Provider       map[string]any    // Raw config parameters for the provider to use. Name determines the specific schema
	SkipCache      bool              // If true, don't use the cache
	SkipAnalytics  bool              // If true, successful requests for this layer don't produce analytics events
	Client         *ClientConfig     // If specified, the default Client is overridden.
}

type Config struct {
	Server         ServerConfig
	Client         ClientConfig
	Logging        LogConfig
	Error          ErrorConfig
	Telemetry      TelemetryConfig
	Secret         map[string]interface{}
	Datastores     []map[string]interface{}
	Authentication map[string]interface{}
	Cache          map[string]interface{}
	Analytics      map[string]interface{}
	Layers         []LayerConfig
}

// Validate checks config fields that are otherwise only caught when the specific code path that
// uses them runs - meaning `config check` could report "Valid" for a config that fails as soon
// as it's actually served. Entity construction (providers, caches, etc.) already happens as part
// of `config check` and catches its own errors; this covers the fields that construction doesn't
// touch: error.mode, logging levels, and logging formats.
func (c Config) Validate() error {
	var errs []error

	switch c.Error.Mode {
	case ModeErrorPlainText, ModeErrorNoError, ModeErrorImage, ModeErrorImageHeader:
	default:
		errs = append(errs, fmt.Errorf("invalid error.mode %q", c.Error.Mode))
	}

	if _, ok := CustomLogLevel[strings.ToLower(c.Logging.Main.Level)]; !ok {
		var level slog.Level
		if err := level.UnmarshalText([]byte(c.Logging.Main.Level)); err != nil {
			errs = append(errs, fmt.Errorf("invalid logging.main.level %q", c.Logging.Main.Level))
		}
	}

	switch c.Logging.Main.Format {
	case MainFormatPlain, MainFormatJSON:
	default:
		errs = append(errs, fmt.Errorf("invalid logging.main.format %q", c.Logging.Main.Format))
	}

	switch c.Logging.Access.Format {
	case AccessFormatCommon, AccessFormatCombined:
	default:
		errs = append(errs, fmt.Errorf("invalid logging.access.format %q", c.Logging.Access.Format))
	}

	return errors.Join(errs...)
}

func DefaultConfig() Config {
	version, _, _ := static.GetVersionInformation()

	return Config{
		Server: ServerConfig{
			BindHost:   "127.0.0.1",
			Port:       8080,
			RootPath:   "/",
			TilePath:   "tiles",
			DocsPath:   "docs",
			Headers:    map[string]string{},
			Production: false,
			Timeout:    60,
			Gzip:       false,
			Health: HealthConfig{
				Enabled: false,
				Port:    3000,
				Host:    "0.0.0.0",
			},
		},
		Telemetry: TelemetryConfig{
			Enabled: false,
		},
		Client: ClientConfig{
			UserAgent:     "tilegroxy/" + version,
			MaxLength:     1024 * 1024 * 10,
			UnknownLength: false,
			// image/png, image/jpg, image/jpeg cover raster tiles; application/vnd.mapbox-vector-tile
			// and application/x-protobuf cover HTTP-proxied MVT sources (mvtContentType in
			// internal/providers/utility.go is the same value tilegroxy's own MVT providers set,
			// duplicated here as a literal since pkg/config can't import an internal/ package).
			// Without these, proxying vector tiles fails until the operator extends this list
			// themselves - a cliff this makes the default no longer trip on.
			ContentTypes:        []string{"image/png", "image/jpg", "image/jpeg", "application/vnd.mapbox-vector-tile", "application/x-protobuf"},
			StatusCodes:         []int{http.StatusOK},
			Headers:             map[string]string{},
			Timeout:             10,
			RewriteContentTypes: map[string]string{"application/octet-stream": ""},
		},
		Logging: LogConfig{
			Main: MainConfig{
				Console: true,
				Path:    "",
				Format:  MainFormatPlain,
				Level:   "info",
				Request: "auto",
				Headers: []string{},
			},
			Access: AccessConfig{
				Console: true,
				Path:    "",
				Format:  AccessFormatCombined,
			},
		},
		Error: ErrorConfig{
			Mode: ModeErrorImage,
			Messages: ErrorMessages{
				NotAuthorized:           "Not authorized",
				InvalidParam:            "Invalid value supplied for parameter %v: %v",
				RangeError:              "%v must be between %v and %v",
				ServerError:             "Unexpected server error: %v",
				ProviderError:           "Provider failed to return image",
				ParamsBothOrNeither:     "Parameters %v and %v must be either both or neither supplied",
				EnumError:               "Invalid value supplied for %v: '%v'. It must be one of: %v",
				ParamsMutuallyExclusive: "Parameters %v and %v cannot both be set",
				ScriptError:             "The script specified for %v is invalid: %v",
				OneOfRequired:           "You must specify one of: %v",
				Timeout:                 "Timeout error",
				ParamRequired:           "Parameter %v is required",
				ParamRegex:              "Invalid value supplied for parameter %v: %v. Value must conform to regex: %v ",
			},
			Images: ErrorImages{
				OutOfBounds:    defaultImageTransparent,
				Authentication: defaultImageUnauthorized,
				Provider:       defaultImageError,
				Other:          defaultImageError,
			},
			AlwaysOK: false,
		},
		Secret: map[string]interface{}{
			"name": "none",
		},
		Datastores: []map[string]interface{}{},
		Authentication: map[string]interface{}{
			"name": "none",
		},
		Cache: map[string]interface{}{
			"name": "none",
		},
		Analytics: map[string]interface{}{
			"name": "none",
		},
		Layers: []LayerConfig{},
	}
}

// DecodeEntityConfig decodes a raw entity config map (as parsed from YAML/JSON provider, cache,
// datastore, authentication, health check, or secret blocks) into the entity-specific config
// struct returned by that entity's InitializeConfig(). It errors on unknown keys so a typo'd
// config field isn't silently ignored - such as a security control quietly reverting to its
// default. "name" picks the registration in the surrounding construction code and no entity
// config struct declares its own Name field, so it's stripped before decoding rather than
// treated as unused. "id" is left as-is: most entities don't use it, but at least one
// (datastore) has its own ID field that needs to be populated from it.
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

// registerDefaults makes every scalar in DefaultConfig() addressable via SetDefault, so
// AutomaticEnv can resolve an environment variable like SERVER_PORT even when server.port is
// absent from the config file entirely. Without this, viper's key set (and therefore which env
// vars AutomaticEnv will look up) comes only from keys already present in the loaded file, so
// "environment variables override config parameters" silently didn't hold for any key the
// operator hadn't already written down.
func registerDefaults(v *viper.Viper) {
	b, err := json.Marshal(DefaultConfig())
	if err != nil {
		// DefaultConfig() is a fixed, statically known struct - a marshal failure here would be
		// a programming error, not a runtime condition callers need to handle.
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

func unmarshal(viper *viper.Viper) (Config, error) {
	c := DefaultConfig()

	// Viper merges a list of maps into a single map key-by-key, so an analytics section written as a list
	// decodes without error into a silent mixture of its entries. Caught here because it's the shape
	// analytics used during development and the failure is otherwise invisible
	if _, ok := viper.Get("analytics").([]interface{}); ok {
		return c, errors.New("analytics must be a single entry, not a list. Remove the leading '- ' and unindent the parameters beneath it")
	}

	err := viper.Unmarshal(&c, func(dc *viperMapstructure.DecoderConfig) {
		dc.ErrorUnused = true
	})
	if err != nil {
		return c, err
	}

	return c, nil
}

func LoadConfig(config string) (Config, error) {
	viper := initViper()

	if strings.Index(strings.TrimSpace(config), "{") == 0 {
		viper.SetConfigType("json")
	} else {
		viper.SetConfigType("yaml")
	}

	err := viper.ReadConfig(bytes.NewBufferString(config))
	if err != nil {
		return Config{}, err
	}

	return unmarshal(viper)
}

func LoadAndWatchConfigFromFile(filename string, onReload func(Config, error)) (Config, error) {
	viper := initViper()

	viper.SetConfigFile(filename)

	err := viper.ReadInConfig()

	if err != nil {
		return Config{}, err
	}

	if onReload != nil {
		lastConfigLoad := time.Now()

		viper.OnConfigChange(func(_ fsnotify.Event) {
			// Avoid duplicate file change events https://github.com/spf13/viper/issues/609
			if time.Since(lastConfigLoad) < time.Second {
				return
			}

			lastConfigLoad = time.Now()

			// Do the reload in a separate thread than the main notify thread to avoid the delay below interfering with the dedupe logic above
			go func() {
				// fsnotify can send events before file has finished writing - give it a second to settle... this might need to be extended to a retry-with-exp-backoff in the future - https://github.com/spf13/viper/issues/1085
				time.Sleep(time.Second)

				err := viper.ReadInConfig()

				if err != nil {
					onReload(Config{}, err)
				} else {
					onReload(unmarshal(viper))
				}
			}()
		})
		viper.WatchConfig()
	}

	return unmarshal(viper)
}

func LoadConfigFromFile(filename string) (Config, error) {
	return LoadAndWatchConfigFromFile(filename, nil)
}

func LoadConfigFromRemote(provider, endpoint, path, format string) (Config, error) {
	viper := initViper()

	viper.SetConfigType(format)
	err := viper.AddRemoteProvider(provider, endpoint, path)

	if err != nil {
		return Config{}, err
	}

	err = viper.ReadRemoteConfig()

	if err != nil {
		return Config{}, err
	}

	return unmarshal(viper)
}
