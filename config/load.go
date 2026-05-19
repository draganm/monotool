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

		defer f.Close()

		cfg := &Config{}
		err = yaml.NewDecoder(f).Decode(cfg)
		if err != nil {
			return nil, fmt.Errorf("could not decode %s: %w", configPath, err)
		}
		cfg.ProjectRoot = dir

		for name, img := range cfg.Images {
			count := 0
			if img.Go != nil {
				count++
			}
			if img.Docker != nil {
				count++
			}
			if count > 1 {
				return nil, fmt.Errorf("image %q has more than one of go/docker configuration, exactly one is required", name)
			}
			if count == 0 {
				return nil, fmt.Errorf("image %q has none of go/docker configuration, exactly one is required", name)
			}
		}

		return cfg, nil

	}

	return nil, errors.New("could not find .monotool/config.yaml in any parent of the curent directory")
}
