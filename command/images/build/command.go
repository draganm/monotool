package build

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/draganm/gosha/gosha"
	"github.com/draganm/monotool/config"
	"github.com/draganm/monotool/docker"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "build",
		Action: func(ctx *cli.Context) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}
			imageNames := lo.Keys(cfg.Images)
			sort.Strings(imageNames)

			for _, cn := range imageNames {

				image := cfg.Images[cn]

				fmt.Println(cn + ":")
				if image.Go == nil {
					return errors.New("no go configuration for the container found")
				}

				sha, err := gosha.CalculatePackageSHA(filepath.Join(cfg.ProjectRoot, image.Go.Package), false, false)
				if err != nil {
					return fmt.Errorf("could not calculate sha of the go module: %w", err)
				}

				fmt.Printf("\tmodule sha: %x\n", sha)

				imageWithTag := fmt.Sprintf("%s:%x", image.DockerImage, sha[:5])
				fmt.Println("\timage name:", imageWithTag)

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				err = docker.Pull(ctx, imageWithTag)
				if err == docker.ErrImageNotFound {
					err = docker.BuildGoMod(ctx, image.Go.Package, imageWithTag)
					if err != nil {
						cancel()
						return err
					}
					cancel()
					continue
				}

				if err != nil {
					cancel()
					return fmt.Errorf("while pulling image: %w", err)
				}
				cancel()

			}

			return nil

		},
	}
}
