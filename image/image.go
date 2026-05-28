package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"

	"github.com/draganm/gosha/gosha"
	"github.com/draganm/monotool/docker"
)

type Image struct {
	Go          *GoImage     `yaml:"go"`
	Docker      *DockerImage `yaml:"docker"`
	DockerImage string       `yaml:"dockerImage"`
	Platform    string       `yaml:"platform"`
}

type GoImage struct {
	Package string `yaml:"package"`
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

func (i *Image) calculateTag(projectRoot string) (string, error) {
	switch {
	case i.Go != nil:
		sha, err := gosha.CalculatePackageSHA(filepath.Join(projectRoot, i.Go.Package), false, false)
		if err != nil {
			return "", fmt.Errorf("could not calculate sha of the go module: %w", err)
		}
		return fmt.Sprintf("%x", sha[:8]), nil
	case i.Docker != nil:
		contextDir, dockerfilePath := i.Docker.resolvePaths(projectRoot)
		tag, err := docker.HashBuildContext(contextDir, dockerfilePath)
		if err != nil {
			return "", fmt.Errorf("could not hash docker build context: %w", err)
		}
		return tag, nil
	default:
		return "", errors.New("no go or docker configuration for the image found")
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
	tag, err := i.calculateTag(projectRoot)
	if err != nil {
		return "", fmt.Errorf("could not calculate hash: %w", err)
	}

	return fmt.Sprintf("%s:%s", i.DockerImage, tag), nil
}

func (i *Image) Build(ctx context.Context, projectRoot string, out io.Writer) error {
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

		err = docker.BuildGoMod(ctx, path.Join(projectRoot, i.Go.Package), imageWithTag, platform, out)
		if err != nil {
			return fmt.Errorf("while building image %s: %w", imageWithTag, err)
		}
	case i.Docker != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}
		contextDir, dockerfilePath := i.Docker.resolvePaths(projectRoot)
		err = docker.BuildDockerfile(ctx, contextDir, dockerfilePath, imageWithTag, platform, out)
		if err != nil {
			return fmt.Errorf("while building docker image %s: %w", imageWithTag, err)
		}
	}

	return nil
}
