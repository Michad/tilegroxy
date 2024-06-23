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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/spf13/viper"
)

type ServerConfig struct {
	BindHost      string            //IP address to bind HTTP server to
	Port          int               //Port to bind HTTP server to
	RootPath      string            //Root HTTP Path to apply to all endpoints. Defaults to /
	TilePath      string            //HTTP Path to serve tiles under (in addition to RootPath). Defaults to tiles which means /tiles/{layer}/{z}/{x}/{y}.
	StaticHeaders map[string]string //Include these headers in all response from server
	Production    bool              //Controls serving splash page, documentation, x-powered-by header. Defaults to false, set true to harden for prod
	Timeout       uint              //How long (in seconds) a request can be in flight before we cancel it and return an error
	Gzip          bool
}

type ClientConfig struct {
	UserAgent           string            //The user agent to include in outgoing http requests. Separate from StaticHeaders to avoid omitting this.
	MaxResponseLength   int               //The maximum Content-Length to allow incoming responses. Default: 10 Megabytes
	AllowUnknownLength  bool              //If true, allow responses that are missing a Content-Length header, this could lead to memory overruns. Default: false
	AllowedContentTypes []string          //The content-types to allow servers to return. Anything else will be interpreted as an error
	AllowedStatusCodes  []int             //The status codes from the remote server to consider successful.  Defaults to just 200
	StaticHeaders       map[string]string //Include these headers in requests. Defaults to none
}

// Modes for error reporting
const (
	ModeErrorPlainText   = "text"         //Response will be text/plain with the error message in the body
	ModeErrorNoError     = "none"         //Response will not include any data but wil return status code.
	ModeErrorImage       = "image"        //Response will return an image but not the error itself
	ModeErrorImageHeader = "image+header" //Response will return an image and include the error inside x-error-message
)

// This is a poor-man's i8n solution. It allows replacing the error messages our app generates in the main `serve` mode.
// It's questionable if anyone will ever want to make use of it, but it at least helps avoid magic strings and can be
// replaced with fully static constants later if it does turn out nobody ever sees value in it
type ErrorMessages struct {
	NotAuthorized           string
	InvalidParam            string
	RangeError              string
	ServerError             string
	ProviderError           string
	ParamsBothOrNeither     string
	ParamsMutuallyExclusive string
	EnumError               string
}

// Selects what image to return when various errors occur. These should either be an embedded:XXX value reflecting an image in `internal/layers/images` or the path to an image in the runtime filesystem
type ErrorImages struct {
	OutOfBounds    string //A request for a zoom level or tile coordinate that's invalid for the requested layer
	Authentication string //Auth failed
	Provider       string //Provider specific errors
	Other          string //Catch-all for unexpected system errors
}

type ErrorConfig struct {
	Mode               string        //How errors should be returned.  See the consts above for options
	Messages           ErrorMessages //Patterns to use for error messages in logs and responses. Not used for utility commands.
	Images             ErrorImages   //Only used if Mode is image or image+header
	SuppressStatusCode bool          //If set we always return 200 regardless of what happens
}

// Formats for outputting the access log
const (
	AccessLogFormatCommon   = "common"
	AccessLogFormatCombined = "combined"
)

type AccessLogConfig struct {
	EnableStandardOut bool   //If true, write access logs to standard out. Defaults to true
	Path              string //The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format            string //The format to output access logs in. Applies to both standard out and file out. Possible values: common, combined. Defaults to common
}

// Formats for outputting the main log
const (
	MainLogFormatPlain = "plain"
	MainLogFormatJson  = "json"
)

const LevelTrace slog.Level = slog.LevelDebug - 5
const LevelAbsurd slog.Level = slog.LevelDebug - 10

var CustomLogLevel = map[string]slog.Level{
	"trace":  LevelTrace,
	"absurd": LevelAbsurd,
}

type MainLogConfig struct {
	EnableStandardOut        bool     //If true, write access logs to standard out. Defaults to true
	Path                     string   //The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format                   string   //The format to output access logs in. Applies to both standard out and file out. Possible values: plain, json. Defaults to plain
	Level                    string   //logging level. one of: debug, info, warn, error, trace, absurd
	IncludeRequestAttributes string   //Can be "true", "false" or "auto". If false, don't include any extra attributes based on request parameters (excluding the ones requested below). If auto (default) it defaults true if format is json, false otherwise
	IncludeHeaders           []string //Headers to include in the logs. Useful for a transaction/request/trace/correlation ID or user identifiers
}

type LogConfig struct {
	AccessLog AccessLogConfig
	MainLog   MainLogConfig
}

type LayerConfig struct {
	Id             string
	Provider       map[string]any
	SkipCache      bool
	OverrideClient *ClientConfig //If specified, all of the default Client is overridden. TODO: re-apply default
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
	version, _, _ := internal.GetVersionInformation()

	return Config{
		Server: ServerConfig{
			BindHost: "127.0.0.1",
			Port:     8080,
			RootPath: "/",
			TilePath: "tiles",
			StaticHeaders: map[string]string{
				"x-test": "true",
			},
			Production: false,
			Timeout:    60,
			Gzip:       false,
		},
		Client: ClientConfig{
			UserAgent:           "tilegroxy/" + version,
			MaxResponseLength:   1024 * 1024 * 10,
			AllowUnknownLength:  false,
			AllowedContentTypes: []string{"image/png", "image/jpg", "image/jpeg"},
			AllowedStatusCodes:  []int{200},
			StaticHeaders:       map[string]string{},
		},
		Logging: LogConfig{
			MainLog: MainLogConfig{
				EnableStandardOut:        true,
				Path:                     "",
				Format:                   MainLogFormatPlain,
				Level:                    "info",
				IncludeRequestAttributes: "auto",
				IncludeHeaders:           []string{},
			},
			AccessLog: AccessLogConfig{
				EnableStandardOut: true,
				Path:              "",
				Format:            AccessLogFormatCombined,
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
			},
			Images: ErrorImages{
				OutOfBounds:    images.KeyImageTransparent,
				Authentication: images.KeyImageUnauthorized,
				Provider:       images.KeyImageError,
				Other:          images.KeyImageError,
			},
			SuppressStatusCode: false,
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

func LoadConfigFromFile(filename string) (Config, error) {
	c := DefaultConfig()
	var viper = viper.New()
	viper.SetConfigFile(filename)

	err := viper.ReadInConfig()

	if err != nil {
		return c, err
	}

	err = viper.Unmarshal(&c)
	if err != nil {
		return c, err
	}

	return c, nil
}
