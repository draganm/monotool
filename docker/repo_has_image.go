package docker

import (
	"context"
	"fmt"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
)

type notFoundInterface interface {
	NotFound()
}

func RepoHasImage(ctx context.Context, image string) (bool, error) {
	cli, err := command.NewDockerCli()
	if err != nil {
		return false, fmt.Errorf("could not create docker client: %w", err)
	}

	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return false, fmt.Errorf("could not parse image name: %w", err)
	}
	_, err = cli.RegistryClient(true).GetManifest(ctx, ref)
	if err != nil {
		_, isNotFound := err.(notFoundInterface)

		if isNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
