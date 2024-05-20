package config

import "github.com/spf13/viper"

type ServerConfig struct {
	BindHost      string            //IP address to bind HTTP server to
	Port          int               //Port to bind HTTP server to
	ContextRoot   string            //Root HTTP Path to server tiles under. Defaults to /tiles which means /tiles/{layer}/{z}/{x}/{y}.
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

const (
	ErrorPlainText = "TEXT"
)

type ErrorMessages struct {
	InvalidParam  string
	RangeError    string
	ServerError   string
	ProviderError string
}

type ErrorConfig struct {
	Mode     string //How errors should be returned.  See the consts above for options TODO: support returning an image in case of error and putting error in the header, also support JSON
	Messages ErrorMessages
}

const (
	AccessLogFormatCommon   = "common"
	AccessLogFormatCombined = "combined"
)

type AccessLogConfig struct {
	EnableStandardOut bool   //If true, write access logs to standard out. Defaults to true
	Path              string //The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format            string //The format to output access logs in. Applies to both standard out and file out. Possible values: common, combined. Defaults to common
}

const (
	MainLogFormatPlain = "plain"
	MainLogFormatJson  = "json"
)

type MainLogConfig struct {
	EnableStandardOut bool   //If true, write access logs to standard out. Defaults to true
	Path              string //The file location to write logs to. Log rotation is not built-in, use an external tool to avoid excessive growth. Defaults to none
	Format            string //The format to output access logs in. Applies to both standard out and file out. Possible values: plain, json. Defaults to plain
	Level             string
}

type LogConfig struct {
	AccessLog AccessLogConfig
	MainLog   MainLogConfig
}

type LayerConfig struct {
	Id             string
	Provider       map[string]any
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

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			BindHost:    "127.0.0.1",
			Port:        8080,
			ContextRoot: "/tiles",
			StaticHeaders: map[string]string{
				"x-test": "true",
			},
			Production: false,
			Timeout:    60,
			Gzip:       false,
		},
		Client: ClientConfig{
			UserAgent:           "tilegroxy/0.0.1", //TODO: make version number dynamic
			MaxResponseLength:   1024 * 1024 * 10,
			AllowUnknownLength:  false,
			AllowedContentTypes: []string{"image/png", "image/jpg"},
			AllowedStatusCodes:  []int{200},
			StaticHeaders:       map[string]string{},
		},
		Logging: LogConfig{
			MainLog: MainLogConfig{
				EnableStandardOut: true,
				Path:              "",
				Format:            MainLogFormatPlain,
				Level:             "info",
			},
			AccessLog: AccessLogConfig{
				EnableStandardOut: true,
				Path:              "",
				Format:            AccessLogFormatCombined,
			},
		},
		Error: ErrorConfig{
			Mode: ErrorPlainText,
			Messages: ErrorMessages{
				InvalidParam:  "Invalid value supplied for parameter %v: %v",
				RangeError:    "Parameter %v must be between %v and %v",
				ServerError:   "Unexpected server error: %v",
				ProviderError: "Provider failed to return image",
			},
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
	c := defaultConfig()
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
