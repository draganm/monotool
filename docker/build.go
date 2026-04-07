package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// BuildDockerfile builds a Docker image from the given context directory and
// Dockerfile path using `docker buildx build`. The resulting image is tagged
// with imageName and loaded into the local Docker daemon.
func BuildDockerfile(ctx context.Context, contextDir, dockerfilePath, imageName, platform string) error {
	if info, err := os.Stat(contextDir); err != nil {
		return fmt.Errorf("stat context dir %s: %w", contextDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("context path is not a directory: %s", contextDir)
	}
	if _, err := os.Stat(dockerfilePath); err != nil {
		return fmt.Errorf("stat dockerfile %s: %w", dockerfilePath, err)
	}

	cmd := exec.CommandContext(ctx, "docker", "buildx", "build",
		"--platform", platform,
		"-t", imageName,
		"-f", dockerfilePath,
		"--progress", "plain",
		contextDir,
	)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed (%w):\n%s", err, out.String())
	}
	return nil
}
