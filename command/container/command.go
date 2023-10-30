package container

import (
	"github.com/draganm/monotool/command/container/status"
	"github.com/urfave/cli/v2"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "container",
		Subcommands: []*cli.Command{
			status.Command(),
		},
	}
}
