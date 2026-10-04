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
	"log/slog"
	"net/http"

	"github.com/Michad/tilegroxy/internal/static"
)

// TLS is enabled when configured, using either a static certificate and keyfile or ACME/Let's Encrypt
type EncryptionConfig struct {
	Domain      string // The domain name you're operating with (the domain end-users use). Required
	Cache       string // The path to a directory to cache certificates in if using let's encrypt. Defaults to ./certs
	Certificate string // The file path to get to the TLS certificate
	KeyFile     string // The file path to get to the keyfile
	HTTPPort    int    // The port used for non-encrypted traffic. Required if using Let's Encrypt for ACME challenge and needs to indirectly be 80 (that is, it could be 8080 if something else redirects 80 to 8080). Everything except .well-known will be redirected to the main port when set.
}

type HealthConfig struct {
	Enabled bool             // If set to false the port isn't bound to. Defaults false
	Port    int              // The port to serve health on. Defaults to 3000
	Host    string           // The host to bind to. Defaults to 0.0.0.0
	Checks  []map[string]any // An array defining the specific checks to perform.
}

// TileJSON documents describing configured layers
type TileJSONConfig struct {
	Enabled   bool     // If true, serve TileJSON documents for eligible layers. Defaults false
	IndexPath string   // The HTTP path, relative to RootPath, that serves a TileJSON index listing every eligible layer. Defaults to tilejson.json
	BaseURLs  []string // Overrides the scheme, host, and path prefix used to build the `tiles` URLs in a document, instead of reading it from forwarding headers or the request itself. Each entry produces one URL in the `tiles` array
}

type DataType string

const (
	DataTypeRaster  DataType = "raster"
	DataTypeMVT     DataType = "mvt"
	DataTypeMLT     DataType = "mlt"
	DataTypeUnknown DataType = "unknown"
)

type BoundsConfig struct {
	South float64
	North float64
	West  float64
	East  float64
}

type CORSConfig struct {
	Enabled          bool     // If true, apply CORS headers and answer preflight requests
	WildcardOrigin   bool     // If true, return Access-Control-Allow-Origin: * and ignore Origins
	Origins          []string // Origins to allow, optionally including scheme and port. The host may use * to match one subdomain or ** to match several. The request Origin is echoed back when it matches
	Methods          []string // Methods to report in preflight responses. Defaults to GET, HEAD, OPTIONS
	Headers          []string // Request headers to allow in preflight responses. A single entry of * echoes back whatever the browser asks for. Defaults to none
	ExposedHeaders   []string // Response headers to make readable to browser scripts. Defaults to ETag
	AllowCredentials bool     // If true, allow credentialed requests. Cannot be combined with WildcardOrigin
	MaxAge           uint     // How long (in seconds) a browser may cache a preflight response. Defaults to 0 which omits the header
}

const (
	CacheVisibilityPublic  = "public"
	CacheVisibilityPrivate = "private"
)

type CacheControlConfig struct {
	Enabled              *bool    // If true, return a Cache-Control header on tile responses. Defaults false
	Auto                 *bool    // If false, don't derive any directive from the layer's configuration. Defaults true
	MaxAge               *uint    // How long (in seconds) a browser may reuse a tile. Returned as max-age. Replaces the derived remaining lifetime
	SharedMaxAge         *uint    // How long (in seconds) a shared cache may reuse a tile. Returned as s-maxage. Never derived
	StaleWhileRevalidate *uint    // How long (in seconds) a cache may serve an expired tile while revalidating. Returned as stale-while-revalidate. Never derived
	Visibility           string   // Either public or private. Replaces the derived visibility
	NoStore              *bool    // If true, return no-store alone. Cannot be combined with the other fields
	Extra                []string // Additional directives returned as-is. Always appended
}

type ServerConfig struct {
	Encrypt      *EncryptionConfig  // Whether and how to use TLS. Defaults to none AKA no encryption.
	TileJSON     TileJSONConfig     // Whether to enable endpoints that describe layers using the TileJSON format
	CORS         CORSConfig         // Whether and how to return cross-origin resource sharing headers
	CacheControl CacheControlConfig // Whether and how to return a Cache-Control header on tile responses
	BindHost     string             // IP address to bind HTTP server to
	Port         int                // Port to bind HTTP server to
	RootPath     string             // Root HTTP Path to apply to all endpoints. Defaults to /
	TilePath     string             // HTTP Path to serve tiles under (in addition to RootPath). Defaults to tiles which means /tiles/{layer}/{z}/{x}/{y}.
	DocsPath     string             // HTTP Path for accessing the documentation website. Defaults to docs
	Headers      map[string]string  // Include these headers in all response from server
	Production   bool               // Controls serving splash page, documentation, x-powered-by header. Defaults to true, set false to expose them outside prod
	Timeout      uint               // How long (in seconds) a request can be in flight before we cancel it and return an error
	Gzip         bool               // Whether to apply gzip compression. Not super helpful when just serving up raster images

	ShutdownTimeout uint // How long (in seconds) the whole shutdown sequence gets. Defaults to Timeout plus DrainDelay.
	DrainDelay      uint // How long (in seconds) to report unready before draining. Defaults to 5, set 0 when a preStop hook covers it.
}

type ClientConfig struct {
	UserAgent           string            // The user agent to include in outgoing http requests. Separate from Headers to avoid omitting this.
	MaxLength           int               // The maximum Content-Length to allow incoming responses. Default: 10 Megabytes
	UnknownLength       *bool             // If true, allow responses that are missing a Content-Length header. Default: false.
	ContentTypes        []string          // The content-types to allow servers to return. Anything else will be interpreted as an error
	StatusCodes         []int             // The status codes from the remote server to consider successful.  Defaults to just 200
	Headers             map[string]string // Include these headers in requests. Defaults to none
	Timeout             uint              // How long (in seconds) a request can be in flight before we cancel it and return an error
	RewriteContentTypes map[string]string // Replace ContentType's that match the key with the value. This is to handle servers returning a generic content type. Kicks in after the check that ContentType is in `ContentTypes`.

}

type TelemetryConfig struct {
	Enabled bool
}

// Error reporting modes
const (
	ModeErrorPlainText   = "text"         // Response will be text/plain with the error message in the body
	ModeErrorNoError     = "none"         // Response will not include any data but will return status code.
	ModeErrorImage       = "image"        // Response will return an image but not the error itself
	ModeErrorImageHeader = "image+header" // Response will return an image and include the error inside x-error-message
)

// A poor-man's i18n that also avoids magic strings. Could become static constants if nobody uses it
type ErrorMessages struct {
	NotAuthorized           string
	ParamRequired           string
	InvalidParam            string
	RangeError              string
	ServerError             string
	ProviderError           string
	ParamsBothOrNeither     string
	ParamsMutuallyExclusive string
	ParamRequiresParam      string
	OneOfRequired           string
	EnumError               string
	ScriptError             string
	Timeout                 string
	ParamRegex              string
	MustBeUnique            string
	TileNotFound            string
}

// Mirrored from internal/images since it imports this package
const (
	defaultImageError        = "embedded:error.png"
	defaultImageTransparent  = "embedded:transparent.png"
	defaultImageUnauthorized = "embedded:unauthorized.png"
	defaultImageMvtEmpty     = "embedded:empty.mvt"
	defaultImageMltEmpty     = "embedded:empty.mlt"
)

// Images returned for various errors. Either embedded:XXX from internal/images or a path on the runtime filesystem
type ErrorImages struct {
	OutOfBounds    string // A request for a zoom level or tile coordinate that's invalid for the requested layer
	Authentication string // Auth failed. Always PNG, see above.
	Provider       string // Provider specific errors
	Other          string // Catch-all for unexpected system errors

	OutOfBoundsMvt string // Vector-tile equivalent of OutOfBounds, used when the layer's data type is mvt
	ProviderMvt    string // Vector-tile equivalent of Provider, used when the layer's data type is mvt
	OtherMvt       string // Vector-tile equivalent of Other, used when the layer's data type is mvt

	OutOfBoundsMlt string // MapLibre Tile equivalent of OutOfBounds, used when the layer's data type is mlt
	ProviderMlt    string // MapLibre Tile equivalent of Provider, used when the layer's data type is mlt
	OtherMlt       string // MapLibre Tile equivalent of Other, used when the layer's data type is mlt
}

type ErrorConfig struct {
	Mode     string        // How errors should be returned.  See the consts above for options
	Messages ErrorMessages // Patterns to use for error messages in logs and responses. Not used for utility commands.
	Images   ErrorImages   // Only used if Mode is image or image+header
	AlwaysOK bool          // If set we always return 200 regardless of what happens
}

// Access log formats
const (
	AccessFormatCommon   = "common"
	AccessFormatCombined = "combined"
)

type AccessConfig struct {
	Console bool   // If true, write access logs to standard out. Defaults to true
	Path    string // The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string // The format to output access logs in. Applies to both standard out and file out. Possible values: common, combined. Defaults to common
}

// Main log formats
const (
	MainFormatPlain = "plain"
	MainFormatJSON  = "json"
)

const LevelTrace slog.Level = slog.LevelDebug - 5
const LevelAbsurd slog.Level = slog.LevelDebug - 10

type MainConfig struct {
	Console bool     // If true, write access logs to standard out. Defaults to true
	Path    string   // The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string   // The format to output access logs in. Applies to both standard out and file out. Possible values: plain, json. Defaults to plain
	Level   string   // logging level. one of: debug, info, warn, error, trace, absurd
	Request string   // Can be "true", "false" or "auto". If false, don't include any extra attributes based on request parameters (excluding the ones requested below). If auto (default) it defaults true if format is json, false otherwise
	Headers []string // Headers to include in the logs. Useful for a transaction/request/trace/correlation ID or user identifiers
}

// Audit log formats
const (
	AuditFormatPlain = "plain"
	AuditFormatJSON  = "json"
)

// Security relevant events: authentication and authorization failures and configuration reloads
type AuditConfig struct {
	Enabled bool     // If true, emit audit events. Defaults to false
	Console bool     // If true, write audit events to standard out. Defaults to true
	Path    string   // The file location to write audit events to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string   // The format to output audit events in. Applies to both standard out and file out. Possible values: plain, json. Defaults to json
	Headers []string // Headers to include as attributes on audit events. Useful for a transaction/request/trace/correlation ID
}

type LogConfig struct {
	Access AccessConfig
	Main   MainConfig
	Audit  AuditConfig
}

type LayerConfig struct {
	ID             string            // A distinct identifier for this layer. If no pattern is defined this is used to match against the layer name. Also used
	Pattern        string            // A pattern to match against for layer names in incoming requests. Includes placeholders from which values can be extracted when matching. Not regular expressions, placeholders are simply wrapped in curly braces
	ParamValidator map[string]string // A mapping of regular expressions to use for each value extracted from the pattern. Keys must match the placeholders in pattern. This is external from the pattern itself to keep parsing the pattern simple and less error prone. If a key of "*" is defined it applies to all placeholders
	Provider       map[string]any    // Raw config parameters for the provider to use. Name determines the specific schema
	SkipCache      bool              // If true, don't use the cache
	SkipAnalytics  bool              // If true, successful requests for this layer don't produce analytics events
	Client         *ClientConfig     // If specified, the default Client is overridden.
	LayerMetadata  `mapstructure:",squash" yaml:",inline"`
	Examples       []string            // Optional. Concrete layer names used to generate TileJSON documents for a `pattern` layer. Has no effect on a layer identified by a plain id
	CacheVersion   string              // Optional. Allows invalidating cache entries when changed. Prefixed into cache keys but not he actual layer name
	Cache          string              // Optional. The id of a top-level cache to use instead of the default
	AllowCoalesce  *bool               // Optional. Whether two requests that come in at the same time for the same tile should be combined. Defaults to auto, which is determined by whether caching is enabled
	CacheControl   *CacheControlConfig // Optional. Overrides the server's Cache-Control block field by field for this layer
}

type Config struct {
	Server         ServerConfig
	Health         HealthConfig
	Client         ClientConfig
	Logging        LogConfig
	Error          ErrorConfig
	Telemetry      TelemetryConfig
	Secret         map[string]interface{}
	Datastores     []map[string]interface{}
	Authentication map[string]interface{}
	Cache          interface{} // Either a single cache or an array of caches.
	DefaultCache   string      // The id of the cache layers use when they don't specify one. Defaults to the first entry
	Analytics      map[string]interface{}
	Layers         []LayerConfig
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
			Production: true,
			Timeout:    60,
			Gzip:       false,
			DrainDelay: 5,
			TileJSON: TileJSONConfig{
				Enabled:   false,
				IndexPath: "tilejson.json",
			},
			CORS: CORSConfig{
				Enabled:        false,
				Methods:        []string{http.MethodGet, http.MethodHead, http.MethodOptions},
				ExposedHeaders: []string{"ETag"},
			},
			CacheControl: CacheControlConfig{
				Enabled: new(false),
				Auto:    new(true),
			},
		},
		Health: HealthConfig{
			Enabled: false,
			Port:    3000,
			Host:    "0.0.0.0",
		},
		Telemetry: TelemetryConfig{
			Enabled: false,
		},
		Client: ClientConfig{
			UserAgent:           "tilegroxy/" + version,
			MaxLength:           1024 * 1024 * 10,
			UnknownLength:       new(false),
			ContentTypes:        []string{"image/png", "image/jpg", "image/jpeg", "application/vnd.mapbox-vector-tile", "application/x-protobuf", "application/vnd.maplibre-tile", "application/vnd.maplibre-vector-tile"},
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
			Audit: AuditConfig{
				Enabled: false,
				Console: true,
				Path:    "",
				Format:  AuditFormatJSON,
				Headers: []string{},
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
				ParamRequiresParam:      "Parameter %v can only be set when %v is enabled",
				ScriptError:             "The script specified for %v is invalid: %v",
				OneOfRequired:           "You must specify one of: %v",
				Timeout:                 "Timeout error",
				ParamRequired:           "Parameter %v is required",
				ParamRegex:              "Invalid value supplied for parameter %v: %v. Value must conform to regex: %v ",
				MustBeUnique:            "Invalid value supplied for parameter %v: %v. Value must be unique. ",
				TileNotFound:            "Tile %v is not available",
			},
			Images: ErrorImages{
				OutOfBounds:    defaultImageTransparent,
				Authentication: defaultImageUnauthorized,
				Provider:       defaultImageError,
				Other:          defaultImageError,

				OutOfBoundsMvt: defaultImageMvtEmpty,
				ProviderMvt:    defaultImageMvtEmpty,
				OtherMvt:       defaultImageMvtEmpty,

				OutOfBoundsMlt: defaultImageMltEmpty,
				ProviderMlt:    defaultImageMltEmpty,
				OtherMlt:       defaultImageMltEmpty,
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
