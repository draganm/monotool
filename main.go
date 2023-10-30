package main

import (
	"fmt"

	initcommand "github.com/draganm/monotool/command/init"
	"github.com/draganm/monotool/config"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Commands: []*cli.Command{
			initcommand.Command(),
		},
		Action: func(ctx *cli.Context) error {
			_, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}

			return nil
		},
	}
	app.RunAndExitOnError()
}
