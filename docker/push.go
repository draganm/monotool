package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

func Push(ctx context.Context, image string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("could not find docker binary: %w", err)
	}
	cmd := exec.CommandContext(ctx, dockerPath, "image", "push", "-q", image)

	out := new(bytes.Buffer)

	cmd.Stderr = out
	cmd.Stdout = out
	err = cmd.Run()

	if err != nil {
		return fmt.Errorf("could not push image (%s): %w", out.String(), err)
	}

	return nil

}
