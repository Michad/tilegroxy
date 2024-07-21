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
	"log/slog"
	"strings"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/spf13/viper"
	_ "github.com/spf13/viper/remote"
)

// Configuration for TLS (HTTPS) operation. If this is configured then TLS is enabled. This can operate either with a static certificate and keyfile via the filesystem or via ACME/Let's Encrypt
type EncryptionConfig struct {
	Domain      string //The domain name you're operating with (the domain end-users use). Required
	Cache       string //The path to a directory to cache certificates in if using let's encrypt. Defaults to ./certs
	Certificate string //The file path to get to the TLS certificate
	KeyFile     string //The file path to get to the keyfile
	HttpPort    int    //The port used for non-encrypted traffic. Required if using Let's Encrypt for ACME challenge and needs to indirectly be 80 (that is, it could be 8080 if something else redirects 80 to 8080). Everything except .well-known will be redirected to the main port when set.
}

type ServerConfig struct {
	Encrypt    *EncryptionConfig //Whether and how to use TLS. Defaults to none AKA no encryption.
	BindHost   string            //IP address to bind HTTP server to
	Port       int               //Port to bind HTTP server to
	RootPath   string            //Root HTTP Path to apply to all endpoints. Defaults to /
	TilePath   string            //HTTP Path to serve tiles under (in addition to RootPath). Defaults to tiles which means /tiles/{layer}/{z}/{x}/{y}.
	Headers    map[string]string //Include these headers in all response from server
	Production bool              //Controls serving splash page, documentation, x-powered-by header. Defaults to false, set true to harden for prod
	Timeout    uint              //How long (in seconds) a request can be in flight before we cancel it and return an error
	Gzip       bool              //Whether to apply gzip compression. Not super helpful when just serving up raster images
}

type ClientConfig struct {
	UserAgent     string            //The user agent to include in outgoing http requests. Separate from Headers to avoid omitting this.
	MaxLength     int               //The maximum Content-Length to allow incoming responses. Default: 10 Megabytes
	UnknownLength bool              //If true, allow responses that are missing a Content-Length header, this could lead to memory overruns. Default: false
	ContentTypes  []string          //The content-types to allow servers to return. Anything else will be interpreted as an error
	StatusCodes   []int             //The status codes from the remote server to consider successful.  Defaults to just 200
	Headers       map[string]string //Include these headers in requests. Defaults to none
	Timeout       uint              //How long (in seconds) a request can be in flight before we cancel it and return an error
}

// TODO: handle this better. Not foolproof in detecting default values and very manual. Probably need to do a mapstructure method for this
func (c *ClientConfig) MergeDefaultsFrom(o ClientConfig) {
	if c.UserAgent == "" {
		c.UserAgent = o.UserAgent
	}
	if c.MaxLength == 0 {
		c.MaxLength = o.MaxLength
	}
	if c.ContentTypes == nil || len(c.ContentTypes) == 0 {
		c.ContentTypes = o.ContentTypes
	}
	if c.StatusCodes == nil || len(c.StatusCodes) == 0 {
		c.StatusCodes = o.StatusCodes
	}
	if c.Timeout == 0 {
		c.Timeout = o.Timeout
	}
}

// Modes for error reporting
const (
	ModeErrorPlainText   = "text"         //Response will be text/plain with the error message in the body
	ModeErrorNoError     = "none"         //Response will not include any data but will return status code.
	ModeErrorImage       = "image"        //Response will return an image but not the error itself
	ModeErrorImageHeader = "image+header" //Response will return an image and include the error inside x-error-message
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
}

// Selects what image to return when various errors occur. These should either be an embedded:XXX value reflecting an image in `internal/layers/images` or the path to an image in the runtime filesystem
type ErrorImages struct {
	OutOfBounds    string //A request for a zoom level or tile coordinate that's invalid for the requested layer
	Authentication string //Auth failed
	Provider       string //Provider specific errors
	Other          string //Catch-all for unexpected system errors
}

type ErrorConfig struct {
	Mode     string        //How errors should be returned.  See the consts above for options
	Messages ErrorMessages //Patterns to use for error messages in logs and responses. Not used for utility commands.
	Images   ErrorImages   //Only used if Mode is image or image+header
	AlwaysOk bool          //If set we always return 200 regardless of what happens
}

// Formats for outputting the access log
const (
	AccessFormatCommon   = "common"
	AccessFormatCombined = "combined"
)

type AccessConfig struct {
	Console bool   //If true, write access logs to standard out. Defaults to true
	Path    string //The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string //The format to output access logs in. Applies to both standard out and file out. Possible values: common, combined. Defaults to common
}

// Formats for outputting the main log
const (
	MainFormatPlain = "plain"
	MainFormatJson  = "json"
)

const LevelTrace slog.Level = slog.LevelDebug - 5
const LevelAbsurd slog.Level = slog.LevelDebug - 10

var CustomLogLevel = map[string]slog.Level{
	"trace":  LevelTrace,
	"absurd": LevelAbsurd,
}

type MainConfig struct {
	Console bool     //If true, write access logs to standard out. Defaults to true
	Path    string   //The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format  string   //The format to output access logs in. Applies to both standard out and file out. Possible values: plain, json. Defaults to plain
	Level   string   //logging level. one of: debug, info, warn, error, trace, absurd
	Request string   //Can be "true", "false" or "auto". If false, don't include any extra attributes based on request parameters (excluding the ones requested below). If auto (default) it defaults true if format is json, false otherwise
	Headers []string //Headers to include in the logs. Useful for a transaction/request/trace/correlation ID or user identifiers
}

type LogConfig struct {
	Access AccessConfig
	Main   MainConfig
}

// Defines a layer to be served up by the application
type LayerConfig struct {
	Id             string            //A distinct identifier for this layer. If no pattern is defined this is used to match against the layer name. Also used
	Pattern        string            //A pattern to match against for layer names in incoming requests. Includes placeholders from which values can be extracted when matching. Not regular expressions, placeholders are simply wrapped in curly braces
	ParamValidator map[string]string //A mapping of regular expressions to use for each value extracted from the pattern. Keys must match the placeholders in pattern. This is external from the pattern itself to keep parsing the pattern simple and less error prone. If a key of "*" is defined it applies to all placeholders
	Provider       map[string]any    //Raw config parameters for the provider to use. Name determines the specific schema
	SkipCache      bool              //If true, don't use the cache
	Client         *ClientConfig     //If specified, the default Client is overridden.
}

type Config struct {
	Server         ServerConfig
	Client         ClientConfig
	Logging        LogConfig
	Error          ErrorConfig
	Authentication map[string]interface{}
	Cache          map[string]interface{}
	Layers         []LayerConfig
}

func DefaultConfig() Config {
	version, _, _ := pkg.GetVersionInformation()

	return Config{
		Server: ServerConfig{
			BindHost:   "127.0.0.1",
			Port:       8080,
			RootPath:   "/",
			TilePath:   "tiles",
			Headers:    map[string]string{},
			Production: false,
			Timeout:    60,
			Gzip:       false,
		},
		Client: ClientConfig{
			UserAgent:     "tilegroxy/" + version,
			MaxLength:     1024 * 1024 * 10,
			UnknownLength: false,
			ContentTypes:  []string{"image/png", "image/jpg", "image/jpeg"},
			StatusCodes:   []int{200},
			Headers:       map[string]string{},
			Timeout:       10,
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
				RangeError:              "Parameter %v must be between %v and %v",
				ServerError:             "Unexpected server error: %v",
				ProviderError:           "Provider failed to return image",
				ParamsBothOrNeither:     "Parameters %v and %v must be either both or neither supplied",
				EnumError:               "Invalid value supplied for %v: '%v'. It must be one of: %v",
				ParamsMutuallyExclusive: "Parameters %v and %v cannot both be set",
				ScriptError:             "The script specified for %v is invalid: %v",
				OneOfRequired:           "You must specify one of: %v",
				Timeout:                 "Timeout error",
				ParamRequired:           "Parameter %v is required",
			},
			Images: ErrorImages{
				OutOfBounds:    images.KeyImageTransparent,
				Authentication: images.KeyImageUnauthorized,
				Provider:       images.KeyImageError,
				Other:          images.KeyImageError,
			},
			AlwaysOk: false,
		},
		Authentication: map[string]interface{}{
			"name": "none",
		},
		Cache: map[string]interface{}{
			"name": "none",
		},
		Layers: []LayerConfig{},
	}
}

func initViper() *viper.Viper {
	var viper = viper.NewWithOptions(viper.KeyDelimiter("_"))
	viper.AutomaticEnv()
	return viper
}

func unmarshal(viper *viper.Viper) (Config, error) {
	c := DefaultConfig()
	err := viper.Unmarshal(&c)
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

func LoadConfigFromFile(filename string) (Config, error) {
	viper := initViper()

	viper.SetConfigFile(filename)

	err := viper.ReadInConfig()

	if err != nil {
		return Config{}, err
	}

	return unmarshal(viper)
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
