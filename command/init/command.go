package init

import (
	"fmt"
	"os"
	"path/filepath"

	_ "embed"

	"github.com/urfave/cli/v2"
)

//go:embed config.yaml
var configYAML []byte

func Command() *cli.Command {
	return &cli.Command{
		Name:        "init",
		Description: "creates .monotool/config.yaml in the current directory",
		Action: func(ctx *cli.Context) error {
			err := os.MkdirAll(".monotool", 0777)
			if err != nil {
				return fmt.Errorf("could not create .monotool dir: %w", err)
			}

			filePath := filepath.Join(".monotool", "config.yaml")

			err = os.WriteFile(filePath, configYAML, 0777)
			if err != nil {
				return fmt.Errorf("could not write %s: %w", filePath, err)
			}

			return nil

		},
	}
}
