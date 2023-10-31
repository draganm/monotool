package image

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/draganm/gosha/gosha"
	"github.com/draganm/monotool/docker"
)

type Image struct {
	Go          *GoImage `yaml:"go"`
	DockerImage string   `yaml:"dockerImage"`
}

type GoImage struct {
	Package string `yaml:"package"`
}

func (i *Image) calculateHash(ctx context.Context, projectRoot string) ([]byte, error) {
	if i.Go == nil {
		return nil, errors.New("no go configuration for the container found")
	}

	sha, err := gosha.CalculatePackageSHA(filepath.Join(projectRoot, i.Go.Package), false, false)
	if err != nil {
		return nil, fmt.Errorf("could not calculate sha of the go module: %w", err)
	}
	return sha, nil
}

func (i *Image) IsAlreadyBuilt(ctx context.Context, projectRoot string) (bool, error) {

	hash, err := i.calculateHash(ctx, projectRoot)
	if err != nil {
		return false, err
	}

	imageWithTag := fmt.Sprintf("%s:%x", i.DockerImage, hash[:8])

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
	hash, err := i.calculateHash(ctx, projectRoot)
	if err != nil {
		return "", fmt.Errorf("could not calculate hash: %w", err)
	}

	imageName := fmt.Sprintf("%s:%x", i.DockerImage, hash[:8])

	return imageName, nil
}

func (i *Image) Build(ctx context.Context, projectRoot string) error {

	imageWithTag, err := i.DockerImageName(ctx, projectRoot)
	if err != nil {
		return err
	}

	err = docker.BuildGoMod(ctx, i.Go.Package, imageWithTag)
	if err == docker.ErrImageNotFound {
		return nil
	}

	if err != nil {
		return fmt.Errorf("while building image: %w", err)
	}

	return nil

}
