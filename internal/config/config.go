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

type LogConfig struct {
	AccessLog bool
	Path      string
}

type Config struct {
	Server         ServerConfig
	Logging        LogConfig
	Authentication map[string]any
	Cache          map[string]any
	Layers         []Layer
}

func defaultConfig() Config {
	return Config {
		Server: ServerConfig{
			BindHost: "127.0.0.1",
			Port: 8080,
			ContextRoot: "/tiles",
			StaticHeaders: map[string]string{
				"x-test": "true",
			},
			Production: false,
			Timeout: 60,
			Gzip: false,
		},
		Logging: LogConfig{
			AccessLog: true,
			Path: "STDOUT",
		},
		Authentication: map[string]any{
			"name": "None",
		},
		Cache: map[string]any{
			"name": "None",
		},
		Layers: []Layer{},
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
