package rollout

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/draganm/monotool/rollout/conflict"
	"github.com/draganm/monotool/rollout/ownership"
)

// helpers
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
}

func setupTemplates(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		mustWrite(t, filepath.Join(dir, name), body)
	}
	return dir
}

func TestGenerateManifestsFirstWrite(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "apiVersion: apps/v1\nkind: Deployment\n",
		"data.json":   `{"a":1}` + "\n",
	})
	workDir := t.TempDir()

	written, conflicts, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("GenerateManifests: %v", err)
	}
	if !conflicts.Empty() {
		t.Fatalf("expected no conflicts, got %d", len(conflicts.Items()))
	}

	sort.Strings(written)
	want := []string{
		filepath.Join(workDir, "apps/staging/data.json"),
		filepath.Join(workDir, "apps/staging/data.json.monotool"),
		filepath.Join(workDir, "apps/staging/deploy.yaml"),
	}
	if !equalSlices(written, want) {
		t.Fatalf("written = %v, want %v", written, want)
	}
}

func TestGenerateManifestsRefusesUnmarked(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	})
	workDir := t.TempDir()
	mustWrite(t, filepath.Join(workDir, "apps/staging/deploy.yaml"), "kind: HumanlyEdited\n")

	_, conflicts, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("GenerateManifests: %v", err)
	}
	if conflicts.Empty() {
		t.Fatal("expected conflicts, got none")
	}
	got := conflicts.Items()
	if len(got) != 1 || got[0].Reason != conflict.ReasonUnmarked {
		t.Fatalf("expected one unmarked conflict, got %+v", got)
	}
}

func TestGenerateManifestsDetectsHashMismatch(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	})
	workDir := t.TempDir()
	target := filepath.Join(workDir, "apps/staging/deploy.yaml")
	mustWriteMarked(t, target, "kind: Deployment\n")

	// Tamper with the body, keep the marker line intact.
	cur, _ := os.ReadFile(target)
	if err := os.WriteFile(target, append(cur, []byte("extra: true\n")...), 0o666); err != nil {
		t.Fatal(err)
	}

	_, conflicts, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
	})
	if err != nil {
		t.Fatalf("GenerateManifests: %v", err)
	}
	got := conflicts.Items()
	if len(got) != 1 || got[0].Reason != conflict.ReasonHashMismatch {
		t.Fatalf("expected one hash-mismatch conflict, got %+v", got)
	}
}

func TestGenerateManifestsForceOverwritesUnmarked(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	})
	workDir := t.TempDir()
	target := filepath.Join(workDir, "apps/staging/deploy.yaml")
	mustWrite(t, target, "kind: HumanlyEdited\n")

	written, _, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
		Force:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != target {
		t.Fatalf("written = %v, want [%s]", written, target)
	}
	body, _ := os.ReadFile(target)
	if !strings.Contains(string(body), "kind: Deployment") {
		t.Fatalf("file was not overwritten: %s", body)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPruneRemovesStaleOwnedFile(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")

	// Pretend last rollout wrote two files.
	staleYAML := filepath.Join(targetDir, "stale.yaml")
	staleJSON := filepath.Join(targetDir, "stale.json")
	mustWriteMarked(t, staleYAML, "kind: Stale\n")
	mustWriteMarked(t, staleJSON, `{"old":true}`+"\n")

	desired := map[string]struct{}{
		filepath.Join(targetDir, "current.yaml"): {},
	}

	removed, conflicts, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: desired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !conflicts.Empty() {
		t.Fatalf("expected no conflicts, got %+v", conflicts.Items())
	}

	sort.Strings(removed)
	want := []string{
		staleJSON,
		staleJSON + ".monotool",
		staleYAML,
	}
	if !equalSlices(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
}

func TestPruneSkipsUnownedFiles(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	human := filepath.Join(targetDir, "human.yaml")
	mustWrite(t, human, "kind: HumanFile\n")

	removed, conflicts, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !conflicts.Empty() {
		t.Fatalf("expected no conflicts, got %+v", conflicts.Items())
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removals, got %v", removed)
	}
	if _, err := os.Stat(human); err != nil {
		t.Fatalf("human file was deleted: %v", err)
	}
}

func TestPruneFlagsHashMismatchAsConflict(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	owned := filepath.Join(targetDir, "edited.yaml")
	mustWriteMarked(t, owned, "kind: Original\n")
	// Tamper.
	cur, _ := os.ReadFile(owned)
	if err := os.WriteFile(owned, append(cur, []byte("extra: true\n")...), 0o666); err != nil {
		t.Fatal(err)
	}

	removed, conflicts, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
		Force:      false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conflicts.Empty() {
		t.Fatal("expected a hash-mismatch conflict")
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removals without --force, got %v", removed)
	}

	// With --force, the file is left alone but the conflict is still reported.
	removedF, conflictsF, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
		Force:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conflictsF.Empty() {
		t.Fatal("expected conflict to still be reported under --force")
	}
	if len(removedF) != 0 {
		t.Fatalf("expected no removals (file is edited), got %v", removedF)
	}
}

func TestPruneRemovesOrphanSidecar(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	sidecar := filepath.Join(targetDir, "ghost.json.monotool")
	mustWrite(t, sidecar, "deadbeef\n")

	removed, conflicts, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !conflicts.Empty() {
		t.Fatalf("orphan sidecar should not produce a conflict; got %+v", conflicts.Items())
	}
	if len(removed) != 1 || removed[0] != sidecar {
		t.Fatalf("expected orphan sidecar removed, got %v", removed)
	}
}

func TestPruneRemovesEmptyDirectories(t *testing.T) {
	workDir := t.TempDir()
	emptyAfter := filepath.Join(workDir, "apps/staging/leftovers")
	stale := filepath.Join(emptyAfter, "x.yaml")
	mustWriteMarked(t, stale, "kind: Stale\n")

	if _, _, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(emptyAfter); !os.IsNotExist(err) {
		t.Fatalf("expected empty dir removed, got err=%v", err)
	}
}

func mustWriteMarked(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := ownership.WriteMarked(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
}
