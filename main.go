package main

import (
	"log"
	"os"

	"github.com/draganm/monotool/command/images"
	initcommand "github.com/draganm/monotool/command/init"
	"github.com/draganm/monotool/command/rollout"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name: "monotool",
		Commands: []*cli.Command{
			initcommand.Command(),
			images.Command(),
			rollout.Command(),
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
