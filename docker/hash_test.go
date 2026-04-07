package docker

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a file at dir/rel with the given content, creating
// parent directories as needed. It fails the test on any I/O error.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func mustHash(t *testing.T, contextDir, dockerfile string) string {
	t.Helper()
	h, err := HashBuildContext(contextDir, dockerfile)
	if err != nil {
		t.Fatalf("HashBuildContext(%s, %s): %v", contextDir, dockerfile, err)
	}
	return h
}

func TestHashBuildContext_StableAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")
	writeFile(t, dir, "main.go", "package main\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)
	h2 := mustHash(t, dir, df)
	if h1 != h2 {
		t.Errorf("expected stable hash, got %q and %q", h1, h2)
	}
}

func TestHashBuildContext_EightByteHex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")

	h := mustHash(t, dir, filepath.Join(dir, "Dockerfile"))
	if len(h) != 16 {
		t.Errorf("expected 16-char hex (8 bytes), got %d chars: %q", len(h), h)
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected lowercase hex, got %q in %q", c, h)
			break
		}
	}
}

func TestHashBuildContext_FileModificationChangesHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")
	writeFile(t, dir, "main.go", "package main\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)

	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	h2 := mustHash(t, dir, df)

	if h1 == h2 {
		t.Errorf("expected hash to change when a context file is modified, got %q twice", h1)
	}
}

func TestHashBuildContext_FileAdditionChangesHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)

	writeFile(t, dir, "new.txt", "hello\n")
	h2 := mustHash(t, dir, df)

	if h1 == h2 {
		t.Errorf("expected hash to change when a file is added, got %q twice", h1)
	}
}

func TestHashBuildContext_FileRemovalChangesHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")
	writeFile(t, dir, "tmp.txt", "hello\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)

	if err := os.Remove(filepath.Join(dir, "tmp.txt")); err != nil {
		t.Fatalf("remove tmp.txt: %v", err)
	}
	h2 := mustHash(t, dir, df)

	if h1 == h2 {
		t.Errorf("expected hash to change when a file is removed, got %q twice", h1)
	}
}

func TestHashBuildContext_DockerignoreExcludesFileFromHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, ".dockerignore", "ignored.txt\n")
	writeFile(t, dir, "ignored.txt", "original\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)

	writeFile(t, dir, "ignored.txt", "changed")
	h2 := mustHash(t, dir, df)

	if h1 != h2 {
		t.Errorf("expected hash NOT to change when ignored file changes, got %q then %q", h1, h2)
	}
}

func TestHashBuildContext_DockerignoreContentAffectsHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, ".dockerignore", "*.txt\n")
	writeFile(t, dir, "data.txt", "data\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)

	// Change .dockerignore patterns — now data.txt is included instead of excluded.
	writeFile(t, dir, ".dockerignore", "*.log\n")
	h2 := mustHash(t, dir, df)

	if h1 == h2 {
		t.Errorf("expected hash to change when .dockerignore patterns change, got %q twice", h1)
	}
}

func TestHashBuildContext_DockerignoreNegationReincludesFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM alpine\n")
	writeFile(t, dir, ".dockerignore", "*.txt\n!keep.txt\n")
	writeFile(t, dir, "keep.txt", "keeper\n")
	writeFile(t, dir, "drop.txt", "dropped\n")

	df := filepath.Join(dir, "Dockerfile")
	h1 := mustHash(t, dir, df)

	// drop.txt is excluded by *.txt with no re-inclusion — changes must not affect hash.
	writeFile(t, dir, "drop.txt", "different")
	h2 := mustHash(t, dir, df)
	if h1 != h2 {
		t.Errorf("expected hash NOT to change when excluded file (drop.txt) changes, got %q then %q", h1, h2)
	}

	// keep.txt is re-included by !keep.txt — changes must affect hash.
	writeFile(t, dir, "keep.txt", "different")
	h3 := mustHash(t, dir, df)
	if h1 == h3 {
		t.Errorf("expected hash to change when re-included file (keep.txt via !) changes, got %q twice", h1)
	}
}

func TestHashBuildContext_DockerfileOutsideContextChanges(t *testing.T) {
	base := t.TempDir()
	ctxDir := filepath.Join(base, "ctx")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir ctx: %v", err)
	}
	writeFile(t, ctxDir, "main.go", "package main\n")

	dockerfilePath := filepath.Join(base, "Dockerfile.prod")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}

	h1 := mustHash(t, ctxDir, dockerfilePath)

	if err := os.WriteFile(dockerfilePath, []byte("FROM ubuntu\n"), 0o644); err != nil {
		t.Fatalf("rewrite dockerfile: %v", err)
	}
	h2 := mustHash(t, ctxDir, dockerfilePath)

	if h1 == h2 {
		t.Errorf("expected hash to change when out-of-context Dockerfile changes, got %q twice", h1)
	}
}

func TestHashBuildContext_MissingDockerfileErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := HashBuildContext(dir, filepath.Join(dir, "Dockerfile"))
	if err == nil {
		t.Error("expected error when Dockerfile is missing, got nil")
	}
}

func TestHashBuildContext_MissingContextErrors(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "nope")
	dockerfile := filepath.Join(base, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}
	_, err := HashBuildContext(missing, dockerfile)
	if err == nil {
		t.Error("expected error when context dir is missing, got nil")
	}
}
