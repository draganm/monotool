package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/draganm/gosha/gosha"
	"github.com/draganm/monotool/docker"
	"github.com/draganm/monotool/nix"
)

type Image struct {
	Go          *GoImage     `yaml:"go"`
	Nix         *NixImage    `yaml:"nix"`
	Docker      *DockerImage `yaml:"docker"`
	DockerImage string       `yaml:"dockerImage"`
	Platform    string       `yaml:"platform"`
}

type GoImage struct {
	Package string `yaml:"package"`
}

type NixImage struct {
	File string `yaml:"file"`
}

// DockerImage builds a container image from an arbitrary Dockerfile + context.
// Context is resolved relative to the project root. Dockerfile is resolved
// relative to the context directory (matching `docker build -f`'s convention)
// and defaults to "Dockerfile" if omitted.
type DockerImage struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// resolvePaths returns the absolute context directory and Dockerfile path for
// a Docker image configuration, applying the default "Dockerfile" name when
// no explicit dockerfile is set.
func (d *DockerImage) resolvePaths(projectRoot string) (contextDir, dockerfilePath string) {
	contextDir = filepath.Join(projectRoot, d.Context)
	dockerfileRel := d.Dockerfile
	if dockerfileRel == "" {
		dockerfileRel = "Dockerfile"
	}
	dockerfilePath = filepath.Join(contextDir, dockerfileRel)
	return contextDir, dockerfilePath
}

func (i *Image) calculateTag(ctx context.Context, projectRoot string) (string, error) {
	switch {
	case i.Go != nil:
		sha, err := gosha.CalculatePackageSHA(filepath.Join(projectRoot, i.Go.Package), false, false)
		if err != nil {
			return "", fmt.Errorf("could not calculate sha of the go module: %w", err)
		}
		return fmt.Sprintf("%x", sha[:8]), nil
	case i.Nix != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}
		drvPath, err := nix.Instantiate(ctx, filepath.Join(projectRoot, i.Nix.File), platform)
		if err != nil {
			return "", fmt.Errorf("could not instantiate nix derivation: %w", err)
		}
		return nix.DrvHash(drvPath), nil
	case i.Docker != nil:
		contextDir, dockerfilePath := i.Docker.resolvePaths(projectRoot)
		tag, err := docker.HashBuildContext(contextDir, dockerfilePath)
		if err != nil {
			return "", fmt.Errorf("could not hash docker build context: %w", err)
		}
		return tag, nil
	default:
		return "", errors.New("no go, nix, or docker configuration for the image found")
	}
}

func (i *Image) IsAlreadyBuilt(ctx context.Context, projectRoot string) (bool, error) {

	imageWithTag, err := i.DockerImageName(ctx, projectRoot)
	if err != nil {
		return false, err
	}

	localExists, err := docker.LocalImageExists(ctx, imageWithTag)
	if err != nil {
		return false, err
	}

	if localExists {
		return true, nil
	}

	err = docker.Pull(ctx, imageWithTag)
	if err == docker.ErrImageNotFound {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("while pulling image: %w", err)
	}

	return true, nil

}

func (i *Image) DockerImageName(ctx context.Context, projectRoot string) (string, error) {
	tag, err := i.calculateTag(ctx, projectRoot)
	if err != nil {
		return "", fmt.Errorf("could not calculate hash: %w", err)
	}

	return fmt.Sprintf("%s:%s", i.DockerImage, tag), nil
}

func (i *Image) Build(ctx context.Context, projectRoot string) error {
	imageWithTag, err := i.DockerImageName(ctx, projectRoot)
	if err != nil {
		return err
	}

	switch {
	case i.Go != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}

		err = docker.BuildGoMod(ctx, path.Join(projectRoot, i.Go.Package), imageWithTag, platform)
		if err != nil {
			return fmt.Errorf("while building image %s: %w", imageWithTag, err)
		}
	case i.Nix != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}
		resultPath, err := nix.Build(ctx, filepath.Join(projectRoot, i.Nix.File), platform)
		if err != nil {
			return fmt.Errorf("while building nix image %s: %w", imageWithTag, err)
		}

		// Clean up temp dir from Docker-based builds (path contains monotool-nix-out-)
		if strings.Contains(resultPath, "monotool-nix-out-") {
			defer os.RemoveAll(filepath.Dir(resultPath))
		}

		err = nix.DockerLoad(ctx, resultPath, imageWithTag)
		if err != nil {
			return fmt.Errorf("while loading nix image %s: %w", imageWithTag, err)
		}
	case i.Docker != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}
		contextDir, dockerfilePath := i.Docker.resolvePaths(projectRoot)
		err = docker.BuildDockerfile(ctx, contextDir, dockerfilePath, imageWithTag, platform)
		if err != nil {
			return fmt.Errorf("while building docker image %s: %w", imageWithTag, err)
		}
	}

	return nil
}
