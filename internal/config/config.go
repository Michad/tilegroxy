package config

import "github.com/spf13/viper"

type Config struct {
	Cache  map[string]any
	Layers []Layer
}

func LoadConfigFromFile(filename string, c *Config) error {
	var viper = viper.New()
	viper.SetConfigFile(filename)

	err := viper.ReadInConfig()

	if err != nil {
		return err
	}

	err = viper.Unmarshal(&c)
	if err != nil {
		return err
	}

	return nil
}
