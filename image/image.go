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
	Go          *GoImage  `yaml:"go"`
	Nix         *NixImage `yaml:"nix"`
	DockerImage string    `yaml:"dockerImage"`
	Platform    string    `yaml:"platform"`
}

type GoImage struct {
	Package string `yaml:"package"`
}

type NixImage struct {
	File string `yaml:"file"`
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
	default:
		return "", errors.New("no go or nix configuration for the image found")
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
	}

	return nil
}
