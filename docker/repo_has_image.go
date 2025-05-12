package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/store"
	registryclient "github.com/docker/cli/cli/registry/client"
	"github.com/docker/docker/api/types/registry"
)

type notFoundInterface interface {
	NotFound()
}

// manifestStoreProvider is used in tests to provide a dummy store.
type manifestStoreProvider interface {
	// ManifestStore returns a store for local manifests
	ManifestStore() store.Store
	RegistryClient(bool) registryclient.RegistryClient
}

// newRegistryClient returns a client for communicating with a Docker distribution
// registry
func newRegistryClient(dockerCLI command.Cli, allowInsecure bool) registryclient.RegistryClient {
	if msp, ok := dockerCLI.(manifestStoreProvider); ok {
		fmt.Println("using manifest store provider")
		// manifestStoreProvider is used in tests to provide a dummy store.
		return msp.RegistryClient(allowInsecure)
	}
	resolver := func(ctx context.Context, index *registry.IndexInfo) registry.AuthConfig {
		return command.ResolveAuthConfig(dockerCLI.ConfigFile(), index)
	}
	return registryclient.NewRegistryClient(resolver, command.UserAgent(), allowInsecure)
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

	list, err := newRegistryClient(cli, true).GetManifestList(ctx, ref)
	if err != nil {
		_, isNotFound := err.(notFoundInterface)

		if isNotFound {
			return false, nil
		}

		if strings.Contains(err.Error(), "is a manifest list") {
			return false, nil
		}

		if strings.Contains(err.Error(), "unsupported manifest format") {
			return false, nil
		}

		return false, err
	}
	return len(list) > 0, nil
}
