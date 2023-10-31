package list

import (
	"context"
	"fmt"
	"sort"

	"github.com/draganm/monotool/config"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "list",
		Action: func(c *cli.Context) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}
			imageNames := lo.Keys(cfg.Images)
			sort.Strings(imageNames)

			ctx := context.Background()

			for _, cn := range imageNames {

				image := cfg.Images[cn]

				fmt.Println(cn + ":")

				imageName, err := image.DockerImageName(ctx, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not calculate image name: %w", err)
				}

				fmt.Println("\tdockerImage:", imageName)

				isBuilt, err := image.IsAlreadyBuilt(ctx, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not determine if image was built: %w", err)
				}
				if !isBuilt {
					fmt.Println("\t❗image has to be rebuilt!")
				}

			}

			return nil

		},
	}
}
