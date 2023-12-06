package helmchart

import (
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

type HelmChart struct {
	Repository  string         `yaml:"repository"`
	Chart       string         `yaml:"chart"`
	Version     string         `yaml:"version"`
	ReleaseName string         `yaml:"releaseName"`
	Values      map[string]any `yaml:"values"`
	SkipCRDs    bool           `yaml:"skipCRDs"`
	Namespace   string         `yaml:"namespace"`
	TargetPath  string         `yaml:"targetPath"`
}

func (h *HelmChart) validate() error {
	if h.Repository == "" {
		return fmt.Errorf("helm chart repository is required")
	}
	if h.Chart == "" {
		return fmt.Errorf("helm chart name is required")
	}
	if h.Version == "" {
		return fmt.Errorf("helm chart version is required")
	}
	if h.ReleaseName == "" {
		return fmt.Errorf("helm chart release name is required")
	}
	if h.Values == nil {
		return fmt.Errorf("helm chart values are required")
	}
	if h.Namespace == "" {
		return fmt.Errorf("helm chart namespace is required")
	}

	if h.TargetPath == "" {
		return fmt.Errorf("helm chart target path is required")
	}
	return nil

}

func (h *HelmChart) GenerateManifests(repositoryCache string) (string, error) {
	err := h.validate()
	if err != nil {
		return "", fmt.Errorf("invalid helm chart definition: %w", err)
	}

	cpo := &action.ChartPathOptions{
		RepoURL: h.Repository,
		Version: h.Version,
	}
	loc, err := cpo.LocateChart(h.Chart, &cli.EnvSettings{
		RepositoryCache: repositoryCache,
	})
	if err != nil {
		return "", fmt.Errorf("failed to locate chart: %w", err)
	}

	ch, err := loader.Load(loc)
	if err != nil {
		return "", fmt.Errorf("failed to load chart: %w", err)
	}

	client := action.NewInstall(&action.Configuration{})
	client.DryRun = true
	client.ReleaseName = h.ReleaseName
	// TODO provide fake kube client and set this to true
	client.ClientOnly = true
	client.Namespace = h.Namespace
	client.IncludeCRDs = true
	client.SkipCRDs = h.SkipCRDs

	res, err := client.Run(ch, h.Values)
	if err != nil {
		return "", fmt.Errorf("failed to run install: %w", err)
	}

	return res.Manifest, nil

}
