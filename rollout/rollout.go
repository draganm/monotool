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

// RollOut runs a full rollout.
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
		written, writeConflicts, err := GenerateManifests(ctx, GenerateOpts{
			TemplatesPath: templatesAbs,
			WorkDir:       workDir,
			TargetPath:    r.TargetPath,
			Values:        values,
			Force:         force,
		})
		if err != nil {
			return nil, nil, err
		}

		var pruneConflicts *conflict.Set
		if r.PruneTargets {
			desired := make(map[string]struct{}, len(written))
			for _, p := range written {
				desired[p] = struct{}{}
			}
			var pruned []string
			pruned, pruneConflicts, err = Prune(ctx, PruneOpts{
				WorkDir:    workDir,
				TargetPath: r.TargetPath,
				DesiredAbs: desired,
				Force:      force,
			})
			if err != nil {
				return nil, nil, err
			}
			removed = pruned
		} else {
			pruneConflicts = conflict.New()
		}

		all := mergeConflicts(writeConflicts, pruneConflicts)
		if !all.Empty() {
			if force {
				all.Warn(os.Stderr)
			} else {
				all.Report(os.Stderr)
				return nil, nil, all.Err()
			}
		}

		return written, removed, nil
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

// PruneOpts drives Prune. DesiredAbs is the set of absolute paths Prune must
// leave alone (typically: every path GenerateManifests wrote, including
// sidecars).
type PruneOpts struct {
	WorkDir    string
	TargetPath string
	DesiredAbs map[string]struct{}
	Force      bool
}

// Prune walks WorkDir/TargetPath and deletes monotool-owned files that are no
// longer in DesiredAbs, returning the absolute paths it removed and any
// conflicts it detected. Unowned files (no marker) are left alone. Owned files
// whose body hash no longer matches their marker generate a hash-mismatch
// conflict; without Force they are not deleted, with Force they are reported
// but still not deleted (we never delete a file a human edited).
func Prune(_ context.Context, opts PruneOpts) (removed []string, conflicts *conflict.Set, err error) {
	conflicts = conflict.New()
	root := filepath.Join(opts.WorkDir, opts.TargetPath)

	if _, statErr := os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
		return nil, conflicts, nil
	}

	type ownedFile struct {
		path string
		rel  string
		st   ownership.FileStatus
	}
	var ownedFiles []ownedFile
	var orphanSidecars []string

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		// Orphan sidecar (sidecar whose JSON sibling doesn't exist) → cruft.
		if filepath.Ext(p) == ownership.SidecarExt {
			jsonPath := p[:len(p)-len(ownership.SidecarExt)]
			if _, statErr := os.Stat(jsonPath); errors.Is(statErr, os.ErrNotExist) {
				orphanSidecars = append(orphanSidecars, p)
			}
			return nil
		}

		st, err := ownership.Status(p)
		if err != nil {
			return err
		}
		if !st.Owned {
			return nil
		}
		rel, err := filepath.Rel(opts.WorkDir, p)
		if err != nil {
			return err
		}
		ownedFiles = append(ownedFiles, ownedFile{path: p, rel: rel, st: st})
		return nil
	})
	if err != nil {
		return nil, conflicts, fmt.Errorf("walk %s: %w", root, err)
	}

	for _, o := range ownedFiles {
		if _, keep := opts.DesiredAbs[o.path]; keep {
			continue
		}

		if !o.st.Matches {
			conflicts.Add(o.rel, conflict.ReasonHashMismatch)
			continue // never delete a file the human edited
		}
		if err := ownership.Remove(o.path); err != nil {
			return removed, conflicts, err
		}
		removed = append(removed, o.path)
		if filepath.Ext(o.path) == ".json" {
			removed = append(removed, o.path+ownership.SidecarExt)
		}
	}

	for _, p := range orphanSidecars {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, conflicts, err
		}
		removed = append(removed, p)
	}

	if err := removeEmptyDirs(root); err != nil {
		return removed, conflicts, err
	}
	return removed, conflicts, nil
}

// removeEmptyDirs walks root bottom-up and removes any directory left empty.
// root itself is not removed.
func removeEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Process deepest-first.
	for i := len(dirs) - 1; i >= 0; i-- {
		if dirs[i] == root {
			continue
		}
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dirs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func mergeConflicts(a, b *conflict.Set) *conflict.Set {
	out := conflict.New()
	for _, c := range a.Items() {
		out.Add(c.Path, c.Reason)
	}
	for _, c := range b.Items() {
		out.Add(c.Path, c.Reason)
	}
	return out
}
