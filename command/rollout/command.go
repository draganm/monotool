package rollout

import (
	"errors"
	"fmt"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/draganm/monotool/config"
	"github.com/draganm/monotool/docker"
	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uiprogress/util/strutil"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

func pointerOf[T any](v T) *T {
	return &v
}

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

			ctx, cancel := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGTERM)
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

					bar.AppendFunc(func(b *uiprogress.Bar) string {
						return fmt.Sprintf("%s| %s", strutil.PadRight(*state.Load(), 23, ' '), imageName)
					})
					state.Store(pointerOf("getting image status"))

					hasImage, err := docker.RepoHasImage(egCtx, imageName)
					if err != nil {
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

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
						state.Store(pointerOf("building image"))
						err = im.Build(egCtx, cfg.ProjectRoot)
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
			err = r.RollOut(ctx, cfg.ProjectRoot, values)
			if err != nil {
				return fmt.Errorf("roll out failed: %w", err)
			}

			return nil

		},
	}
}
