package rollout

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/draganm/manifestor/interpolate"
	"github.com/draganm/monotool/rollout/gitea"
	"gopkg.in/yaml.v3"
)

type Rollout struct {
	Gitea        *gitea.GiteaRollout `yaml:"gitea"`
	Templates    string              `yaml:"templates"`
	TargetPath   string              `yaml:"targetPath"`
	PruneTargets bool                `yaml:"pruneTargets"`
}

func (r *Rollout) RollOut(ctx context.Context, projectRoot string, values map[string]any) error {
	if r.Gitea == nil {
		return errors.New("deployment has no gitea config")
	}

	templatesPath, err := filepath.Abs(filepath.Join(projectRoot, r.Templates))
	if err != nil {
		return fmt.Errorf("could not get absolute path for the deployment templates: %w", err)
	}

	templates := map[string][]byte{}

	err = filepath.WalkDir(templatesPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		ext := filepath.Ext(path)
		if !(ext == ".yaml" || ext == ".yml") {
			return nil
		}

		relativePath, err := filepath.Rel(path, templatesPath)
		if err != nil {
			return fmt.Errorf("could not get relative path of %s: %w", path, err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", path, err)
		}

		templates[relativePath] = data

		return nil
	})

	if err != nil {
		return fmt.Errorf("could not read templates: %w", err)
	}

	generateManifests := func(dir string) error {
		for n, d := range templates {
			manifestPath := filepath.Join(dir, n)
			err := os.MkdirAll(path.Dir(manifestPath), 0777)
			if err != nil {
				return fmt.Errorf("could not mkdir %s: %w", path.Dir(manifestPath), err)
			}
			f, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0777)
			if err != nil {
				return fmt.Errorf("could not open manifest output file %s: %w", manifestPath, err)
			}

			enc := yaml.NewEncoder(f)
			err = interpolate.Interpolate(string(d), manifestPath, values, enc)
			if err != nil {
				f.Close()
				return fmt.Errorf("could not interpolate %s: %w", manifestPath, err)
			}

			err = f.Close()
			if err != nil {
				return fmt.Errorf("could not close %s: %w", manifestPath, err)
			}
		}

		return nil
	}

	err = r.Gitea.RollOut(ctx, generateManifests)
	if err != nil {
		return fmt.Errorf("gitea deployment failed: %w", err)
	}

	return nil

}
