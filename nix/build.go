package nix

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Build runs nix-build on the given nix file and returns the resolved result path
// (the Nix store path the result symlink points to).
// The platform parameter uses Docker format (e.g. "linux/amd64") and is converted
// to a Nix system string. On macOS, when targeting Linux, the build is executed
// inside a Lima VM to avoid the need for a remote Linux builder.
func Build(ctx context.Context, nixFile string, platform string) (string, error) {
	absPath, err := filepath.Abs(nixFile)
	if err != nil {
		return "", fmt.Errorf("could not resolve nix file path: %w", err)
	}

	nixSystem, err := dockerPlatformToNixSystem(platform)
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "darwin" && strings.HasPrefix(platform, "linux/") {
		return buildViaDarwinLima(ctx, absPath, nixSystem)
	}

	return buildNative(ctx, absPath, nixSystem)
}

// buildNative runs nix-build directly on the host.
func buildNative(ctx context.Context, absPath string, nixSystem string) (string, error) {
	cmd := exec.CommandContext(ctx, "nix-build", absPath, "--no-out-link", "--system", nixSystem)
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = errOut

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("nix-build failed (%w):\n%s", err, errOut.String())
	}

	resultPath := strings.TrimSpace(out.String())
	if resultPath == "" {
		return "", fmt.Errorf("nix-build returned empty output")
	}

	if _, err := os.Stat(resultPath); err != nil {
		return "", fmt.Errorf("nix-build result path does not exist: %w", err)
	}

	return resultPath, nil
}

const limaInstanceName = "nix"

// ensureLimaInstance checks that the "nix" Lima instance exists and is running.
// If it doesn't exist, it creates one from the default template and installs Nix.
// If it exists but is stopped, it starts it.
func ensureLimaInstance(ctx context.Context) error {
	// Check if instance exists
	cmd := exec.CommandContext(ctx, "limactl", "list", "-q")
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("limactl list failed (%w):\n%s", err, out.String())
	}

	instances := strings.Split(strings.TrimSpace(out.String()), "\n")
	exists := false
	for _, inst := range instances {
		if strings.TrimSpace(inst) == limaInstanceName {
			exists = true
			break
		}
	}

	if !exists {
		// Create instance from default template
		fmt.Fprintf(os.Stderr, "Creating Lima instance %q (this may take a few minutes)...\n", limaInstanceName)
		cmd = exec.CommandContext(ctx, "limactl", "start", "--name="+limaInstanceName, "template://default", "--tty=false")
		out = new(bytes.Buffer)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("limactl start failed: %w", err)
		}

		// Install Nix in the instance
		fmt.Fprintf(os.Stderr, "Installing Nix in Lima instance %q...\n", limaInstanceName)
		installScript := `sh <(curl -L https://nixos.org/nix/install) --daemon --yes`
		cmd = exec.CommandContext(ctx, "limactl", "shell", limaInstanceName, "--", "sh", "-c", installScript)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("nix installation in lima failed: %w", err)
		}

		return nil
	}

	// Instance exists — check if it's running
	cmd = exec.CommandContext(ctx, "limactl", "list", "--format", "{{.Status}}", "--filter", ".name==\""+limaInstanceName+"\"")
	out = new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("limactl list status failed (%w):\n%s", err, out.String())
	}

	status := strings.TrimSpace(out.String())
	if status != "Running" {
		fmt.Fprintf(os.Stderr, "Starting Lima instance %q...\n", limaInstanceName)
		cmd = exec.CommandContext(ctx, "limactl", "start", limaInstanceName)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("limactl start %s failed: %w", limaInstanceName, err)
		}
	}

	return nil
}

// buildViaDarwinLima runs nix-build inside a Lima VM when on macOS.
// Lima shares the macOS filesystem automatically, so paths work directly.
func buildViaDarwinLima(ctx context.Context, absPath string, nixSystem string) (string, error) {
	if err := ensureLimaInstance(ctx); err != nil {
		return "", fmt.Errorf("could not ensure lima instance: %w", err)
	}

	outDir, err := os.MkdirTemp("", "monotool-nix-out-")
	if err != nil {
		return "", fmt.Errorf("could not create temp output dir: %w", err)
	}

	buildScript := fmt.Sprintf(
		`set -e; result=$(nix-build %s --no-out-link --system %s); cp "$result" %s/result.tar.gz`,
		absPath, nixSystem, outDir,
	)

	cmd := exec.CommandContext(ctx, "limactl", "shell", limaInstanceName, "--", "sh", "-c", buildScript)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out

	err = cmd.Run()
	if err != nil {
		os.RemoveAll(outDir)
		return "", fmt.Errorf("nix-build in lima failed (%w):\n%s", err, out.String())
	}

	resultPath := filepath.Join(outDir, "result.tar.gz")
	if _, err := os.Stat(resultPath); err != nil {
		os.RemoveAll(outDir)
		return "", fmt.Errorf("nix-build lima result not found: %w", err)
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

// dockerPlatformToNixSystem converts a Docker platform string (e.g. "linux/amd64")
// to a Nix system string (e.g. "x86_64-linux").
func dockerPlatformToNixSystem(platform string) (string, error) {
	switch platform {
	case "linux/amd64":
		return "x86_64-linux", nil
	case "linux/arm64":
		return "aarch64-linux", nil
	case "linux/arm/v7":
		return "armv7l-linux", nil
	case "linux/386":
		return "i686-linux", nil
	default:
		return "", fmt.Errorf("unsupported platform for nix build: %s", platform)
	}
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
