package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func Load() (*Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not get working dir: %w", err)
	}

	for filepath.Dir(dir) != dir {
		configPath := filepath.Join(dir, ".monotool", "config.yaml")
		f, err := os.Open(configPath)
		if os.IsNotExist(err) {
			dir = filepath.Dir(dir)
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("failed to stat %s: %w", configPath, err)
		}

		cfg := &Config{}
		err = yaml.NewDecoder(f).Decode(cfg)
		if err != nil {
			return nil, fmt.Errorf("could not decode %s: %w", configPath, err)
		}
		cfg.ProjectRoot = dir

		return cfg, nil

	}

	return nil, errors.New("could not find .monotool/config.yaml in any parent of the curent directory")
}
