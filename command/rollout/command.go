package rollout

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
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

			ctx, cancel := context.WithCancel(context.Background())

			go func() {
				sigs := make(chan os.Signal, 1)
				signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

				sig := <-sigs
				log.Println("signal received, terminating", "sig", sig)
				cancel()
			}()

			images := map[string]string{}
			values := map[string]any{
				"images": images,
			}

			eg, ctx := errgroup.WithContext(ctx)

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

					bar.AppendFunc(func(b *uiprogress.Bar) string {
						return fmt.Sprintf("%s: %s", strutil.PadRight(*state.Load(), 20, ' '), n)
					})
					state.Store(pointerOf("getting image status"))
					isBuilt, err := im.IsAlreadyBuilt(ctx, cfg.ProjectRoot)
					if err != nil {
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}
					di, err := im.DockerImageName(ctx, cfg.ProjectRoot)
					if err != nil {
						return fmt.Errorf("could not calculate docker image of %s: %w", n, err)
					}

					bar.Incr()
					needsPush := false

					if !isBuilt {
						needsPush = true
						state.Store(pointerOf("building image"))
						err = im.Build(ctx, cfg.ProjectRoot)
						if err != nil {
							return err
						}
					}

					bar.Incr()

					if needsPush {
						state.Store(pointerOf("pushing image"))
						err = docker.Push(ctx, di)
						if err != nil {
							return err
						}
					}

					bar.Incr()
					images[n] = di
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
