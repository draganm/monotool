package build

import (
	"fmt"
	"sort"

	"github.com/draganm/monotool/config"
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

				imageName, err := image.DockerImageName(ctx.Context, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not calculate docker image name for %s: %w", cn, err)
				}

				fmt.Println(cn + ":")
				fmt.Println("\timage name:", imageName)

				alreadyBuilt, err := image.IsAlreadyBuilt(ctx.Context, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not check if image %s is already built: %w", cn, err)
				}

				if alreadyBuilt {
					fmt.Println("\talready built")
					continue
				}

				fmt.Println("\tbuilding...")
				err = image.Build(ctx.Context, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not build image %s: %w", cn, err)
				}
				fmt.Println("\tdone")

			}

			return nil

		},
	}
}
