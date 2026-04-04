package nix

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Build runs nix-build on the given nix file and returns the resolved result path
// (the Nix store path the result symlink points to).
func Build(ctx context.Context, nixFile string) (string, error) {
	absPath, err := filepath.Abs(nixFile)
	if err != nil {
		return "", fmt.Errorf("could not resolve nix file path: %w", err)
	}

	cmd := exec.CommandContext(ctx, "nix-build", absPath, "--no-out-link")
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = errOut

	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("nix-build failed (%w):\n%s", err, errOut.String())
	}

	resultPath := strings.TrimSpace(out.String())
	if resultPath == "" {
		return "", fmt.Errorf("nix-build returned empty output")
	}

	// Verify the result path exists
	if _, err := os.Stat(resultPath); err != nil {
		return "", fmt.Errorf("nix-build result path does not exist: %w", err)
	}

	return resultPath, nil
}

// DockerLoad loads a nix-built image tarball into the local Docker daemon
// and tags it with the given image name.
func DockerLoad(ctx context.Context, archivePath string, imageRef string) error {
	cmd := exec.CommandContext(ctx, "docker", "load", "-i", archivePath)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("docker load failed (%w):\n%s", err, out.String())
	}

	// docker load outputs "Loaded image: <name:tag>" or "Loaded image ID: sha256:..."
	// We need to tag the loaded image with our desired reference.
	loadedOutput := out.String()
	loadedImage := parseLoadedImage(loadedOutput)
	if loadedImage == "" {
		return fmt.Errorf("could not parse loaded image from docker load output: %s", loadedOutput)
	}

	cmd = exec.CommandContext(ctx, "docker", "tag", loadedImage, imageRef)
	out = new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("docker tag failed (%w):\n%s", err, out.String())
	}

	return nil
}

// parseLoadedImage extracts the image reference from docker load output.
// Output is typically "Loaded image: name:tag" or "Loaded image ID: sha256:abc123".
func parseLoadedImage(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Loaded image: "); ok {
			return v
		}
		if v, ok := strings.CutPrefix(line, "Loaded image ID: "); ok {
			return v
		}
	}
	return ""
}
