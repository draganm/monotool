package rollout

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/draganm/monotool/config"
	"github.com/draganm/monotool/docker"
	"github.com/draganm/monotool/rollout/confirm"
	"github.com/draganm/monotool/ui"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/term"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "rollout",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "message",
				Aliases:  []string{"m"},
				Usage:    "describe the purpose of the rollout (included in the PR description)",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "overwrite files in the gitops repo that aren't owned by monotool or have been edited since the last rollout",
			},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}

			message := strings.TrimSpace(c.String("message"))
			if message == "" {
				return errors.New("rollout message (-m) must not be empty")
			}

			requestedRollout := c.Args().First()

			buildSemaphore := semaphore.NewWeighted(4)
			checkImageSemaphore := semaphore.NewWeighted(10)

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
					sb.WriteString("there are %d rollouts available, please specify one of the following:\n")
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

			ctx, cancel := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
			defer cancel()

			imageNames := lo.Keys(cfg.Images)
			sort.Strings(imageNames)

			prog := ui.New(imageNames)
			prog.Run()
			prog.WaitForContextCancel(ctx)

			images := map[string]string{}
			values := map[string]any{
				"images": images,
			}
			imagesLock := &sync.Mutex{}

			eg, egCtx := errgroup.WithContext(ctx)

			for _, n := range imageNames {
				n := n
				im := cfg.Images[n]
				eg.Go(func() error {
					if egCtx.Err() != nil {
						return egCtx.Err()
					}

					w := prog.Writer(n)
					prog.SetState(n, "checking remote")

					imageName, err := im.DockerImageName(egCtx, cfg.ProjectRoot)
					if err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not calculate docker image of %s: %w", n, err)
					}

					prog.SetImageName(n, imageName)

					imagesLock.Lock()
					images[n] = imageName
					imagesLock.Unlock()

					if err := checkImageSemaphore.Acquire(egCtx, 1); err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not acquire semaphore for image %s: %w", n, err)
					}

					hasImage, err := docker.RepoHasImage(egCtx, imageName)
					checkImageSemaphore.Release(1)
					if err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

					if hasImage {
						prog.SetState(n, "already pushed")
						prog.Finish(n, nil)
						return nil
					}

					isBuilt, err := im.IsAlreadyBuilt(egCtx, cfg.ProjectRoot)
					if err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

					if !isBuilt {
						if err := buildSemaphore.Acquire(egCtx, 1); err != nil {
							prog.Finish(n, err)
							return fmt.Errorf("could not acquire semaphore for building image %s: %w", n, err)
						}
						prog.SetState(n, "building image")
						err = im.Build(egCtx, cfg.ProjectRoot, w)
						buildSemaphore.Release(1)
						if err != nil {
							prog.Finish(n, err)
							return err
						}
					}

					prog.SetState(n, "pushing image")
					if err := docker.Push(egCtx, imageName, w); err != nil {
						prog.Finish(n, err)
						return err
					}

					prog.SetState(n, "done")
					prog.Finish(n, nil)
					return nil
				})
			}

			buildErr := eg.Wait()
			prog.FinishAll()
			if waitErr := prog.Wait(); waitErr != nil {
				return waitErr
			}
			if buildErr != nil {
				return fmt.Errorf("could not build images: %w", buildErr)
			}

			fmt.Printf("rolling out to %s\n", requestedRollout)

			var conf confirm.Confirmer
			if c.Bool("force") {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return errors.New("--force requires an interactive terminal")
				}
				conf = confirm.TTYConfirmer(os.Stdin, os.Stderr)
				fmt.Fprintln(os.Stderr, "--force review: confirming each destructive action")
			}

			err = r.RollOut(ctx, cfg.ProjectRoot, values, message, c.Bool("force"), conf)
			if errors.Is(err, confirm.ErrAborted) {
				fmt.Fprintln(os.Stderr, "rollout aborted by user")
				return cli.Exit("", 1)
			}
			if err != nil {
				return fmt.Errorf("roll out failed: %w", err)
			}

			return nil
		},
	}
}
