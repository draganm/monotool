package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

func Push(ctx context.Context, image string, out io.Writer) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("could not find docker binary: %w", err)
	}
	cmd := exec.CommandContext(ctx, dockerPath, "image", "push", "-q", image)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not push image: %w", err)
	}
	return nil
}
