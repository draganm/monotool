package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrImageNotFound = errors.New("image not found")

func Pull(ctx context.Context, image string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("could not find docker binary: %w", err)
	}
	cmd := exec.CommandContext(ctx, dockerPath, "image", "pull", "-q", image)

	out := new(bytes.Buffer)

	cmd.Stderr = out
	cmd.Stdout = out
	err = cmd.Run()

	if err != nil {
		if strings.Contains(out.String(), ": not found") {
			return ErrImageNotFound
		}
		return err
	}

	return nil

}
