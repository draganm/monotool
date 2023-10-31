package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

func LocalImageExists(ctx context.Context, image string) (bool, error) {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return false, fmt.Errorf("could not find docker binary: %w", err)
	}
	cmd := exec.CommandContext(ctx, dockerPath, "image", "ls", "-q", image)

	out := new(bytes.Buffer)

	cmd.Stderr = out
	cmd.Stdout = out
	err = cmd.Run()

	if err != nil {
		return false, fmt.Errorf("could not list image %s: %w", out.String(), err)
	}

	return out.String() != "", nil

}
