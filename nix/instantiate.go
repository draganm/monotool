package nix

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Instantiate runs nix-instantiate on the given nix file and returns the derivation store path.
// The platform parameter uses Docker format (e.g. "linux/amd64") and is converted
// to a Nix system string passed as --argstr system.
func Instantiate(ctx context.Context, nixFile string, platform string) (string, error) {
	absPath, err := filepath.Abs(nixFile)
	if err != nil {
		return "", fmt.Errorf("could not resolve nix file path: %w", err)
	}

	nixSystem, err := dockerPlatformToNixSystem(platform)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "nix-instantiate", absPath, "--argstr", "system", nixSystem)
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = errOut

	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("nix-instantiate failed (%w):\n%s", err, errOut.String())
	}

	drvPath := strings.TrimSpace(out.String())
	if drvPath == "" {
		return "", fmt.Errorf("nix-instantiate returned empty output")
	}

	return drvPath, nil
}

// DrvHash extracts the first 8 characters of the hash from a Nix store path.
// A store path looks like /nix/store/<hash>-<name>.drv
func DrvHash(drvPath string) string {
	base := filepath.Base(drvPath)
	// The hash is everything before the first '-'
	if idx := strings.IndexByte(base, '-'); idx > 0 {
		hash := base[:idx]
		if len(hash) > 8 {
			return hash[:8]
		}
		return hash
	}
	return base
}
