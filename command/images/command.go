package images

import (
	"github.com/draganm/monotool/command/images/build"
	"github.com/draganm/monotool/command/images/list"
	"github.com/urfave/cli/v2"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "images",
		Subcommands: []*cli.Command{
			list.Command(),
			build.Command(),
		},
	}
}
