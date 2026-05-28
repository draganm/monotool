package rollout

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/draganm/monotool/config"
	"github.com/draganm/monotool/docker"
	"github.com/draganm/monotool/rollout/confirm"
	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uiprogress/util/strutil"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/term"
)

func pointerOf[T any](v T) *T {
	return &v
}

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

			buildSemapore := semaphore.NewWeighted(4)
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

			ctx, cancel := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
			defer cancel()
			images := map[string]string{}
			values := map[string]any{
				"images": images,
			}
			imagesLock := &sync.Mutex{}

			eg, egCtx := errgroup.WithContext(ctx)

			progress := uiprogress.New()
			progress.RefreshInterval = time.Second
			progress.Width = 20
			progress.Start()

			for n, im := range cfg.Images {
				n := n
				im := im
				eg.Go(func() error {
					if egCtx.Err() != nil {
						return egCtx.Err()
					}

					bar := progress.AddBar(3)
					bar.PrependElapsed()
					bar.TimeStarted = time.Now()

					state := atomic.Pointer[string]{}
					state.Store(pointerOf("initializing"))

					imageName, err := im.DockerImageName(egCtx, cfg.ProjectRoot)
					if err != nil {
						return fmt.Errorf("could not calculate docker image of %s: %w", n, err)
					}

					imagesLock.Lock()
					images[n] = imageName
					imagesLock.Unlock()

					err = checkImageSemaphore.Acquire(egCtx, 1)
					if err != nil {
						return fmt.Errorf("could not acquire semaphore for image %s: %w", n, err)
					}

					bar.AppendFunc(func(b *uiprogress.Bar) string {
						return fmt.Sprintf("%s| %s", strutil.PadRight(*state.Load(), 23, ' '), imageName)
					})
					state.Store(pointerOf("getting image status"))

					hasImage, err := docker.RepoHasImage(egCtx, imageName)
					if err != nil {
						checkImageSemaphore.Release(1)
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

					checkImageSemaphore.Release(1)

					if hasImage {
						bar.Set(3)
						state.Store(pointerOf("already pushed"))
						return nil
					}

					isBuilt, err := im.IsAlreadyBuilt(egCtx, cfg.ProjectRoot)
					if err != nil {
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

					bar.Incr()

					if !isBuilt {
						err = buildSemapore.Acquire(egCtx, 1)
						if err != nil {
							return fmt.Errorf("could not acquire semaphore for building image %s: %w", n, err)
						}
						state.Store(pointerOf("building image"))
						err = im.Build(egCtx, cfg.ProjectRoot)
						buildSemapore.Release(1)
						if err != nil {
							return err
						}
					}

					bar.Incr()

					state.Store(pointerOf("pushing image"))
					err = docker.Push(egCtx, imageName)
					if err != nil {
						return err
					}

					bar.Incr()
					state.Store(pointerOf("done"))

					return nil

				})

			}

			err = eg.Wait()
			progress.Stop()
			if err != nil {
				return fmt.Errorf("could not build images: %w", err)
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
