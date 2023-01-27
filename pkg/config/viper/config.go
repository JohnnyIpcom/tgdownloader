package viper

import (
	"fmt"
	"strings"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

type configViper struct {
	*viper.Viper
	name string
}

var _ config.Config = &configViper{}

func NewConfig() config.Config {
	return &configViper{
		name:  "",
		Viper: viper.New(),
	}
}

func (c *configViper) Load(name string, path string) error {
	if path != "" {
		fmt.Println("Using config file:", path)
		c.Viper.SetConfigFile(path)
	} else {
		fmt.Println("Config file not specified, using default config file path")
		home, err := homedir.Dir()
		if err != nil {
			return err
		}

		c.Viper.AddConfigPath(home)
		c.Viper.AddConfigPath("./")
		c.Viper.SetConfigName(name)
	}

	c.Viper.SetEnvPrefix(name)
	c.Viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	c.Viper.AutomaticEnv()

	if err := c.Viper.ReadInConfig(); err != nil {
		return err
	}

	fmt.Printf("Using config file: %s\n", c.Viper.ConfigFileUsed())
	return nil
}

func (c *configViper) Sub(key string) config.Config {
	subName := fmt.Sprintf("%s_%s", c.name, key)

	cfg := c.Viper.Sub(key)
	cfg.SetEnvPrefix(subName)
	cfg.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	cfg.AutomaticEnv()

	return &configViper{
		Viper: cfg,
		name:  subName,
	}
}

func (c *configViper) Unmarshal(rawVal interface{}) error {
	return c.Viper.Unmarshal(rawVal)
}

func (c *configViper) GetSlice(key string) []interface{} {
	return cast.ToSlice(c.Viper.Get(key))
}
