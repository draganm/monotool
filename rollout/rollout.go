package rollout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/draganm/manifestor/interpolate"
	"github.com/draganm/monotool/rollout/conflict"
	"github.com/draganm/monotool/rollout/gitea"
	"github.com/draganm/monotool/rollout/github"
	"github.com/draganm/monotool/rollout/ownership"
	"gopkg.in/yaml.v3"
)

type Rollout struct {
	Gitea        *gitea.GiteaRollout   `yaml:"gitea"`
	GitHub       *github.GitHubRollout `yaml:"github"`
	Templates    string                `yaml:"templates"`
	TargetPath   string                `yaml:"targetPath"`
	PruneTargets bool                  `yaml:"pruneTargets"`
}

// GenerateOpts is the input to GenerateManifests. It is exposed (and the
// function is exported) so tests can drive the write phase without needing a
// git remote.
type GenerateOpts struct {
	TemplatesPath string
	WorkDir       string
	TargetPath    string
	Values        map[string]any
	Force         bool
}

// GenerateManifests reads templates, interpolates them, and writes the
// resulting YAML/JSON files into WorkDir/TargetPath using ownership markers.
// It returns the absolute paths of every file it wrote (including JSON
// sidecars) and the set of conflicts it detected. If Force is true, conflicts
// are recorded but writes proceed anyway.
func GenerateManifests(_ context.Context, opts GenerateOpts) (written []string, conflicts *conflict.Set, err error) {
	conflicts = conflict.New()

	templates, err := readTemplates(opts.TemplatesPath, opts.TargetPath)
	if err != nil {
		return nil, conflicts, err
	}

	for relPath, raw := range templates {
		fullPath := filepath.Join(opts.WorkDir, relPath)

		body, err := renderTemplate(fullPath, raw, opts.Values)
		if err != nil {
			return written, conflicts, fmt.Errorf("render %s: %w", relPath, err)
		}

		st, err := ownership.Status(fullPath)
		if err != nil {
			return written, conflicts, fmt.Errorf("status %s: %w", fullPath, err)
		}

		if st.Exists && (!st.Owned || !st.Matches) {
			reason := conflict.ReasonUnmarked
			if st.Owned {
				reason = conflict.ReasonHashMismatch
			}
			conflicts.Add(relPath, reason)
			if !opts.Force {
				continue
			}
		}

		if err := ownership.WriteMarked(fullPath, body); err != nil {
			return written, conflicts, err
		}
		written = append(written, fullPath)
		if filepath.Ext(fullPath) == ".json" {
			written = append(written, fullPath+ownership.SidecarExt)
		}
	}

	return written, conflicts, nil
}

// readTemplates walks templatesPath, returning a map keyed by the target-path-
// relative path (e.g., "apps/staging/deploy.yaml") with the raw template body.
// Non-YAML/JSON files and directories are skipped.
func readTemplates(templatesPath, targetPath string) (map[string][]byte, error) {
	absRoot, err := filepath.Abs(templatesPath)
	if err != nil {
		return nil, fmt.Errorf("abs %s: %w", templatesPath, err)
	}

	out := map[string][]byte{}
	err = filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ext := filepath.Ext(p)
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		rel, err := filepath.Rel(absRoot, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read template %s: %w", p, err)
		}
		out[filepath.Join(targetPath, rel)] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk templates: %w", err)
	}
	return out, nil
}

// renderTemplate interpolates a template body using values. JSON files are
// copied verbatim (matching pre-existing monotool behavior). YAML files are
// run through manifestor's interpolator and re-encoded.
func renderTemplate(path string, raw []byte, values map[string]any) ([]byte, error) {
	if filepath.Ext(path) == ".json" {
		return raw, nil
	}
	buf := new(bytes.Buffer)
	enc := yaml.NewEncoder(buf)
	if err := interpolate.Interpolate(string(raw), "", values, enc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RollOut runs a full rollout. The old removeOldManifests + write-everything
// logic is being replaced incrementally; the next task wires the new prune in.
func (r *Rollout) RollOut(ctx context.Context, projectRoot string, values map[string]any, message string, force bool) error {
	if r.Gitea == nil && r.GitHub == nil {
		return errors.New("rollout must have either a gitea or github config")
	}
	if r.Gitea != nil && r.GitHub != nil {
		return errors.New("rollout cannot have both gitea and github configs")
	}

	templatesAbs, err := filepath.Abs(filepath.Join(projectRoot, r.Templates))
	if err != nil {
		return fmt.Errorf("could not get absolute path for the deployment templates: %w", err)
	}

	generate := func(workDir string) (added, removed []string, err error) {
		written, conflicts, err := GenerateManifests(ctx, GenerateOpts{
			TemplatesPath: templatesAbs,
			WorkDir:       workDir,
			TargetPath:    r.TargetPath,
			Values:        values,
			Force:         force,
		})
		if err != nil {
			return written, nil, err
		}
		if !conflicts.Empty() {
			conflicts.Report(os.Stderr)
			if !force {
				return nil, nil, conflicts.Err()
			}
		}

		// Pruning is wired in the next task. For now: no removed paths.
		return written, nil, nil
	}

	switch {
	case r.Gitea != nil:
		if err := r.Gitea.RollOut(ctx, message, generate); err != nil {
			return fmt.Errorf("gitea deployment failed: %w", err)
		}
	case r.GitHub != nil:
		if err := r.GitHub.RollOut(ctx, message, generate); err != nil {
			return fmt.Errorf("github deployment failed: %w", err)
		}
	}
	return nil
}
