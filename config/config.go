package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	AnthropicApiKey string `toml:"anthropic_api_key"`
}

var NoConfigErr = errors.New("no config")

func LoadConfig() (*Config, error) {
	confFile := ConfigFilePath()

	if confFile == "" {
		return &Config{}, NoConfigErr
	}

	tml, err := os.ReadFile(confFile)
	if err != nil {
		return &Config{}, NoConfigErr
	}

	var conf Config
	err = toml.Unmarshal(tml, &conf)
	if err != nil {
		return &Config{}, err
	}

	return &conf, nil
}

func ConfigFilePath() string {
	userConfDir, _ := os.UserConfigDir()
	if userConfDir == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return ""
		}
		userConfDir = filepath.Join(home, ".config")
	}

	return filepath.Join(userConfDir, "code-buddy", "code-buddy.toml")
}
