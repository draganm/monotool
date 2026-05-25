package rollout

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/draganm/monotool/rollout/conflict"
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
