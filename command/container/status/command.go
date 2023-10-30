package status

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
		Name: "status",
		Action: func(ctx *cli.Context) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}
			containerNames := lo.Keys(cfg.Containers)
			sort.Strings(containerNames)

			for _, cn := range containerNames {

				container := cfg.Containers[cn]

				fmt.Println(cn + ":")
				if container.Go == nil {
					return errors.New("no go configuration for the container found")
				}
				sha, err := gosha.CalculatePackageSHA(filepath.Join(cfg.ProjectRoot, container.Go.Package), false, false)
				if err != nil {
					return fmt.Errorf("could not calculate sha of the go module: %w", err)
				}

				fmt.Printf("\tmodule sha: %x\n", sha)

				imageWithTag := fmt.Sprintf("%s:%x", container.Image, sha[:5])
				fmt.Println("\timage name:", imageWithTag)

				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				err = docker.Pull(ctx, imageWithTag)
				if err == docker.ErrImageNotFound {
					fmt.Println("\t❗image has to be rebuilt!")
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
