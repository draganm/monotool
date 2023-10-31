package rollout

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/draganm/monotool/config"
	"github.com/draganm/monotool/docker"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "rollout",
		Action: func(c *cli.Context) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}

			requestedRollout := c.Args().First()

			if requestedRollout == "" {

				switch len(cfg.RollOuts) {
				case 0:
					return errors.New("there are no rollouts defined in the config file")
				case 1:
					for n := range cfg.RollOuts {
						requestedRollout = n
					}
				default:
					allRollouts := lo.Keys(cfg.RollOuts)
					sort.Strings(allRollouts)
					sb := new(strings.Builder)
					sb.WriteString("there are %s rollouts available, please specify one of the following:\n")
					for _, r := range allRollouts {
						sb.WriteString(fmt.Sprintf("%s\n", r))
					}
					return fmt.Errorf(sb.String(), len(cfg.RollOuts))
				}

			}

			r, found := cfg.RollOuts[requestedRollout]
			if !found {
				return fmt.Errorf("rollout %q does not exist", requestedRollout)
			}

			ctx := context.Background()

			images := map[string]string{}
			values := map[string]any{
				"images": images,
			}

			fmt.Println("checking images")

			for n, im := range cfg.Images {
				isBuilt, err := im.IsAlreadyBuilt(ctx, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not get status of image %s: %w", n, err)
				}

				di, err := im.DockerImageName(ctx, cfg.ProjectRoot)
				if err != nil {
					return fmt.Errorf("could not calculate docker image of %s: %w", n, err)
				}

				fmt.Printf("  %s: %s\n", n, di)

				if !isBuilt {
					fmt.Printf("  building %s: ", n)
					err = im.Build(ctx, cfg.ProjectRoot)
					if err != nil {
						return err
					}
					fmt.Printf("  ✅ built\n")
				} else {
					fmt.Printf("  ✅ already built\n")
				}

				fmt.Println("  pushing to docker registry")
				err = docker.Push(ctx, di)
				if err != nil {
					return err
				}
				fmt.Printf("  ✅ pushed\n")

				images[n] = di

				fmt.Println()

			}

			fmt.Printf("rolling out to %s\n", requestedRollout)
			err = r.RollOut(ctx, cfg.ProjectRoot, values)
			if err != nil {
				return fmt.Errorf("roll out failed: %w", err)
			}

			return nil

		},
	}
}
