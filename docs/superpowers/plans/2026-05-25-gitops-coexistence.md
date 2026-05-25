# GitOps Repo Coexistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `monotool rollout` share its gitops repo with other writers (humans, bots) without overwriting or pruning their files; surface conflicts loudly with an opt-in `--force` override.

**Architecture:** Each file monotool writes carries a SHA-256 marker — inline `# monotool-hash:` header for YAML, sidecar `<file>.monotool` for JSON. The rollout flow reads each existing file's marker before writing, collects conflicts (unmarked or hash-mismatch), and aborts unless `--force`. Pruning walks `targetPath` and only deletes files whose markers it can verify. Git staging switches from `git add .` to explicit per-path `git add` / `git rm`.

**Tech Stack:** Go 1.25, stdlib only for new code (`crypto/sha256`, `encoding/hex`, `os`, `path/filepath`, `bytes`, `strings`). Tests use stdlib `testing` with table-driven cases.

**Spec:** [docs/superpowers/specs/2026-05-25-gitops-coexistence-design.md](../specs/2026-05-25-gitops-coexistence-design.md)

---

## File Structure

**New files:**
- `rollout/ownership/ownership.go` — marker read/write, hash computation, file-type dispatch (YAML vs JSON).
- `rollout/ownership/ownership_test.go` — table-driven tests covering all marker behaviors.
- `rollout/conflict/conflict.go` — `Conflict` and `Set` types, report formatting.
- `rollout/conflict/conflict_test.go` — set behavior and report rendering tests.

**Modified files:**
- `rollout/rollout.go` — replace `generateManifests` and `removeOldManifests` with marker-aware versions; change `RollOut` signature to accept `force bool` and return `(added, removed []string, err error)` through the `generate` closure.
- `rollout/rollout_test.go` — new file, integration-ish tests for `RollOut` against a temp dir simulating the gitops working tree.
- `rollout/gitops/operations.go` — add `StageChanges(ctx, dir, added, removed)`, remove `AddFiles`.
- `rollout/gitea/roll_out_to_gitea.go` — accept `force bool`, call `StageChanges` with results from `generate`, skip commit/push/PR when no changes.
- `rollout/github/roll_out_to_github.go` — same changes as gitea.
- `command/rollout/command.go` — add `--force` CLI flag, thread through to `r.RollOut`.

**File responsibilities:**
- `ownership` knows only about marker syntax and hashing. It does not know about templates, rollouts, or git.
- `conflict` knows only about collecting and reporting conflicts. It does not know about files.
- `rollout` orchestrates: it consumes `ownership` and `conflict`, decides what to write/delete, and tells the rollout backend what to stage.
- The backends (`gitea`, `github`) own git plumbing only. They are agnostic to ownership and conflicts.

---

## Conventions used throughout this plan

- Hash format: lowercase hex SHA-256 (64 chars).
- YAML marker line: exactly `# monotool-hash: <hex>\n`, must be the **first line** of the file. Hash is over the bytes **after** the marker line.
- JSON sidecar: file at `<json-path>.monotool` containing the hex hash plus a trailing newline. Hash is over the JSON file's entire body.
- "YAML" means files with extension `.yaml` or `.yml`. "JSON" means files with extension `.json`. Other extensions are not produced by the rollout (templates already filter to these) and need no handling.
- All new exported identifiers start with `Read*`, `Write*`, `Compute*`, `Strip*` — no abbreviations.

---

## Task 1: Set up `rollout/ownership` package with constants

**Files:**
- Create: `rollout/ownership/ownership.go`

Skeleton that other tasks build on. No behavior yet beyond constants and the marker-prefix detector.

- [ ] **Step 1: Create the package file**

```go
package ownership

import (
	"path/filepath"
	"strings"
)

// MarkerPrefix is the prefix of the YAML marker comment. The full line is
// MarkerPrefix + <hex sha256> + "\n".
const MarkerPrefix = "# monotool-hash: "

// SidecarExt is appended to a JSON file path to form its marker sidecar path.
const SidecarExt = ".monotool"

// isYAML reports whether path has a YAML extension.
func isYAML(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}

// isJSON reports whether path has a JSON extension.
func isJSON(path string) bool {
	return filepath.Ext(path) == ".json"
}

// hasMarkerLine reports whether body starts with the YAML marker prefix.
func hasMarkerLine(body []byte) bool {
	return strings.HasPrefix(string(body), MarkerPrefix)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./rollout/ownership/...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add rollout/ownership/ownership.go
git commit -m "ownership: add package skeleton with marker constants"
```

---

## Task 2: Implement hash computation + marker stripping

**Files:**
- Modify: `rollout/ownership/ownership.go`
- Create: `rollout/ownership/ownership_test.go`

- [ ] **Step 1: Write the failing test**

Append to (or create) `rollout/ownership/ownership_test.go`:

```go
package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestStripMarker(t *testing.T) {
	cases := []struct {
		name string
		path string
		in   string
		want string
	}{
		{"yaml without marker", "x.yaml", "apiVersion: v1\n", "apiVersion: v1\n"},
		{"yaml with marker", "x.yaml", "# monotool-hash: abc\napiVersion: v1\n", "apiVersion: v1\n"},
		{"yml with marker", "x.yml", "# monotool-hash: abc\nkind: X\n", "kind: X\n"},
		{"json unchanged", "x.json", `{"a":1}` + "\n", `{"a":1}` + "\n"},
		{"yaml only marker no body", "x.yaml", "# monotool-hash: abc\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripMarker(tc.path, []byte(tc.in))
			if string(got) != tc.want {
				t.Fatalf("StripMarker(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestComputeBodyHash(t *testing.T) {
	body := []byte("apiVersion: v1\nkind: ConfigMap\n")
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])

	// YAML with no marker: hash is over the whole input.
	gotYAML := ComputeBodyHash("x.yaml", body)
	if gotYAML != want {
		t.Fatalf("YAML no-marker hash = %s, want %s", gotYAML, want)
	}

	// YAML with marker: hash is over the body after the marker.
	gotMarked := ComputeBodyHash("x.yaml", append([]byte("# monotool-hash: deadbeef\n"), body...))
	if gotMarked != want {
		t.Fatalf("YAML marked hash = %s, want %s", gotMarked, want)
	}

	// JSON: hash is over the entire file body (no marker possible inline).
	gotJSON := ComputeBodyHash("x.json", body)
	if gotJSON != want {
		t.Fatalf("JSON hash = %s, want %s", gotJSON, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rollout/ownership/...`
Expected: FAIL with "undefined: StripMarker" and "undefined: ComputeBodyHash".

- [ ] **Step 3: Implement `StripMarker` and `ComputeBodyHash`**

Append to `rollout/ownership/ownership.go`:

```go
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// StripMarker returns the portion of body that participates in the hash.
// For YAML files with a marker line as the first line, the marker line and its
// trailing newline are removed. All other inputs are returned unchanged.
func StripMarker(path string, body []byte) []byte {
	if !isYAML(path) || !hasMarkerLine(body) {
		return body
	}
	nl := bytes.IndexByte(body, '\n')
	if nl < 0 {
		return nil
	}
	return body[nl+1:]
}

// ComputeBodyHash returns the lowercase-hex SHA-256 of the hash-relevant
// portion of body (see StripMarker).
func ComputeBodyHash(path string, body []byte) string {
	stripped := StripMarker(path, body)
	sum := sha256.Sum256(stripped)
	return hex.EncodeToString(sum[:])
}
```

Update the existing import block to include `bytes`, `crypto/sha256`, `encoding/hex`. Combine all imports into one block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rollout/ownership/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rollout/ownership/ownership.go rollout/ownership/ownership_test.go
git commit -m "ownership: add StripMarker and ComputeBodyHash"
```

---

## Task 3: Implement `WriteMarked` for YAML and JSON

**Files:**
- Modify: `rollout/ownership/ownership.go`
- Modify: `rollout/ownership/ownership_test.go`

- [ ] **Step 1: Write the failing test**

Append to `rollout/ownership/ownership_test.go`:

```go
import (
	"os"
	"path/filepath"
)

func TestWriteMarkedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy.yaml")
	body := []byte("apiVersion: apps/v1\nkind: Deployment\n")

	if err := WriteMarked(path, body); err != nil {
		t.Fatalf("WriteMarked: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	wantHash := ComputeBodyHash(path, body)
	wantHead := MarkerPrefix + wantHash + "\n"
	if string(written[:len(wantHead)]) != wantHead {
		t.Fatalf("file head = %q, want %q", written[:len(wantHead)], wantHead)
	}
	if string(written[len(wantHead):]) != string(body) {
		t.Fatalf("file body after marker = %q, want %q", written[len(wantHead):], body)
	}
}

func TestWriteMarkedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := []byte(`{"a":1}` + "\n")

	if err := WriteMarked(path, body); err != nil {
		t.Fatalf("WriteMarked: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile json: %v", err)
	}
	if string(written) != string(body) {
		t.Fatalf("JSON body modified: got %q, want %q", written, body)
	}

	sidecar, err := os.ReadFile(path + SidecarExt)
	if err != nil {
		t.Fatalf("ReadFile sidecar: %v", err)
	}
	wantHash := ComputeBodyHash(path, body)
	if string(sidecar) != wantHash+"\n" {
		t.Fatalf("sidecar = %q, want %q", sidecar, wantHash+"\n")
	}
}

func TestWriteMarkedCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested/dir/x.yaml")
	if err := WriteMarked(path, []byte("a: b\n")); err != nil {
		t.Fatalf("WriteMarked nested: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rollout/ownership/...`
Expected: FAIL with "undefined: WriteMarked".

- [ ] **Step 3: Implement `WriteMarked`**

Append to `rollout/ownership/ownership.go`:

```go
import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteMarked writes body to path and records monotool's ownership marker.
// For YAML files, the marker is prepended as a comment line. For JSON files,
// the marker is stored in a sidecar file named <path>+SidecarExt. Parent
// directories are created as needed.
func WriteMarked(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	hash := ComputeBodyHash(path, body)

	switch {
	case isYAML(path):
		out := make([]byte, 0, len(MarkerPrefix)+len(hash)+1+len(body))
		out = append(out, MarkerPrefix...)
		out = append(out, hash...)
		out = append(out, '\n')
		out = append(out, body...)
		if err := os.WriteFile(path, out, 0o666); err != nil {
			return fmt.Errorf("write yaml %s: %w", path, err)
		}
	case isJSON(path):
		if err := os.WriteFile(path, body, 0o666); err != nil {
			return fmt.Errorf("write json %s: %w", path, err)
		}
		if err := os.WriteFile(path+SidecarExt, []byte(hash+"\n"), 0o666); err != nil {
			return fmt.Errorf("write sidecar %s: %w", path+SidecarExt, err)
		}
	default:
		return fmt.Errorf("WriteMarked: unsupported extension for %s", path)
	}
	return nil
}
```

Merge all imports into a single block at the top of the file (`bytes`, `crypto/sha256`, `encoding/hex`, `fmt`, `os`, `path/filepath`, `strings`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rollout/ownership/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rollout/ownership/ownership.go rollout/ownership/ownership_test.go
git commit -m "ownership: write marked YAML and JSON files"
```

---

## Task 4: Implement `ReadMarker` + ownership status helpers

**Files:**
- Modify: `rollout/ownership/ownership.go`
- Modify: `rollout/ownership/ownership_test.go`

This task adds the read side and a single status function callers will use.

- [ ] **Step 1: Write the failing test**

Append to `rollout/ownership/ownership_test.go`:

```go
func TestStatusOwnedAndUnmodified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.yaml")
	body := []byte("kind: X\n")
	if err := WriteMarked(path, body); err != nil {
		t.Fatal(err)
	}
	st, err := Status(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Owned {
		t.Fatal("expected Owned=true")
	}
	if !st.Matches {
		t.Fatal("expected Matches=true")
	}
}

func TestStatusUnmarkedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(path, []byte("kind: X\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	st, err := Status(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Owned {
		t.Fatal("expected Owned=false")
	}
}

func TestStatusHashMismatchYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := WriteMarked(path, []byte("a: 1\n")); err != nil {
		t.Fatal(err)
	}
	// Tamper with the body while leaving the marker line intact.
	cur, _ := os.ReadFile(path)
	tampered := append([]byte{}, cur...)
	tampered = append(tampered, []byte("extra: true\n")...)
	if err := os.WriteFile(path, tampered, 0o666); err != nil {
		t.Fatal(err)
	}

	st, err := Status(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Owned {
		t.Fatal("expected Owned=true (marker still present)")
	}
	if st.Matches {
		t.Fatal("expected Matches=false (body tampered)")
	}
}

func TestStatusUnmarkedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o666); err != nil {
		t.Fatal(err)
	}
	st, err := Status(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Owned {
		t.Fatal("expected Owned=false (no sidecar)")
	}
}

func TestStatusHashMismatchJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := WriteMarked(path, []byte(`{"a":1}`+"\n")); err != nil {
		t.Fatal(err)
	}
	// Tamper with the JSON, leave sidecar alone.
	if err := os.WriteFile(path, []byte(`{"a":2}`+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	st, err := Status(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Owned {
		t.Fatal("expected Owned=true (sidecar present)")
	}
	if st.Matches {
		t.Fatal("expected Matches=false (body tampered)")
	}
}

func TestStatusMalformedMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	// Marker prefix is present but the hash is not valid hex.
	body := []byte(MarkerPrefix + "not-a-real-hash-value\nkind: X\n")
	if err := os.WriteFile(path, body, 0o666); err != nil {
		t.Fatal(err)
	}
	st, err := Status(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Owned {
		t.Fatal("expected Owned=true (marker prefix present)")
	}
	if st.Matches {
		t.Fatal("expected Matches=false (malformed marker counts as mismatch)")
	}
}

func TestStatusMissingFile(t *testing.T) {
	dir := t.TempDir()
	st, err := Status(filepath.Join(dir, "gone.yaml"))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Exists {
		t.Fatal("expected Exists=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rollout/ownership/...`
Expected: FAIL with "undefined: Status".

- [ ] **Step 3: Implement `Status` and `ReadMarker`**

Append to `rollout/ownership/ownership.go`:

```go
import "encoding/hex"

// FileStatus describes a target path's ownership state.
type FileStatus struct {
	// Exists reports whether the file is present on disk.
	Exists bool
	// Owned reports whether a monotool marker is present (YAML header or JSON
	// sidecar). False when Exists is false.
	Owned bool
	// Matches reports whether the recorded marker hash equals the current
	// body's hash. False when Owned is false or when the marker is malformed.
	Matches bool
}

// Status inspects path and returns its ownership status. A missing file
// produces FileStatus{} (all false) with a nil error. I/O errors other than
// "not exist" are returned.
func Status(path string) (FileStatus, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileStatus{}, nil
	}
	if err != nil {
		return FileStatus{}, fmt.Errorf("read %s: %w", path, err)
	}

	st := FileStatus{Exists: true}

	markerHash, owned, err := readMarker(path, body)
	if err != nil {
		return FileStatus{}, err
	}
	st.Owned = owned
	if !owned {
		return st, nil
	}

	if !isHex64(markerHash) {
		return st, nil // owned but malformed → Matches stays false
	}

	st.Matches = ComputeBodyHash(path, body) == markerHash
	return st, nil
}

// readMarker returns the recorded hash and whether a marker was found.
// For YAML, the marker is the first-line comment. For JSON, the marker is the
// sidecar file. body is the file body for YAML; for JSON it is unused.
func readMarker(path string, body []byte) (hash string, owned bool, err error) {
	switch {
	case isYAML(path):
		if !hasMarkerLine(body) {
			return "", false, nil
		}
		nl := bytes.IndexByte(body, '\n')
		if nl < 0 {
			return "", true, nil
		}
		line := string(body[:nl])
		return strings.TrimSpace(strings.TrimPrefix(line, MarkerPrefix)), true, nil
	case isJSON(path):
		side, err := os.ReadFile(path + SidecarExt)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("read sidecar %s: %w", path+SidecarExt, err)
		}
		return strings.TrimSpace(string(side)), true, nil
	default:
		return "", false, nil
	}
}

// isHex64 reports whether s is exactly 64 lowercase hex characters.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	if _, err := hex.DecodeString(s); err != nil {
		return false
	}
	return true
}
```

Add `errors` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rollout/ownership/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rollout/ownership/ownership.go rollout/ownership/ownership_test.go
git commit -m "ownership: add Status for marker inspection"
```

---

## Task 5: Add `Remove` helper for owned files

**Files:**
- Modify: `rollout/ownership/ownership.go`
- Modify: `rollout/ownership/ownership_test.go`

A small wrapper that deletes a YAML file or a JSON file + its sidecar in one call. Used by prune.

- [ ] **Step 1: Write the failing test**

Append to `rollout/ownership/ownership_test.go`:

```go
func TestRemoveYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := WriteMarked(path, []byte("a: 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file removed")
	}
}

func TestRemoveJSONIncludesSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := WriteMarked(path, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected JSON removed")
	}
	if _, err := os.Stat(path + SidecarExt); !os.IsNotExist(err) {
		t.Fatal("expected sidecar removed")
	}
}

func TestRemoveMissingIsNoError(t *testing.T) {
	dir := t.TempDir()
	if err := Remove(filepath.Join(dir, "gone.yaml")); err != nil {
		t.Fatalf("Remove missing: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rollout/ownership/...`
Expected: FAIL with "undefined: Remove".

- [ ] **Step 3: Implement `Remove`**

Append to `rollout/ownership/ownership.go`:

```go
// Remove deletes the file at path and, for JSON files, its sidecar. Missing
// files are not an error.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if isJSON(path) {
		if err := os.Remove(path + SidecarExt); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove sidecar %s: %w", path+SidecarExt, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rollout/ownership/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rollout/ownership/ownership.go rollout/ownership/ownership_test.go
git commit -m "ownership: add Remove helper for owned files and sidecars"
```

---

## Task 6: Create `rollout/conflict` package

**Files:**
- Create: `rollout/conflict/conflict.go`
- Create: `rollout/conflict/conflict_test.go`

- [ ] **Step 1: Write the failing test**

Create `rollout/conflict/conflict_test.go`:

```go
package conflict

import (
	"bytes"
	"strings"
	"testing"
)

func TestEmptySet(t *testing.T) {
	s := New()
	if !s.Empty() {
		t.Fatal("expected empty set")
	}
	if s.Err() != nil {
		t.Fatal("expected nil Err on empty set")
	}
}

func TestAddAndErr(t *testing.T) {
	s := New()
	s.Add("apps/foo/x.yaml", ReasonUnmarked)
	if s.Empty() {
		t.Fatal("expected non-empty set")
	}
	if s.Err() == nil {
		t.Fatal("expected error on non-empty set")
	}
}

func TestReportGroupsByReason(t *testing.T) {
	s := New()
	s.Add("apps/foo/a.yaml", ReasonUnmarked)
	s.Add("apps/foo/b.json", ReasonUnmarked)
	s.Add("apps/foo/c.yaml", ReasonHashMismatch)

	buf := new(bytes.Buffer)
	s.Report(buf)
	out := buf.String()

	if !strings.Contains(out, "unmarked") {
		t.Errorf("want 'unmarked' header, got: %s", out)
	}
	if !strings.Contains(out, "hash-mismatch") {
		t.Errorf("want 'hash-mismatch' header, got: %s", out)
	}
	for _, p := range []string{"a.yaml", "b.json", "c.yaml"} {
		if !strings.Contains(out, p) {
			t.Errorf("want %q in report, got: %s", p, out)
		}
	}
}

func TestReportPathsSortedWithinReason(t *testing.T) {
	s := New()
	s.Add("apps/z.yaml", ReasonUnmarked)
	s.Add("apps/a.yaml", ReasonUnmarked)

	buf := new(bytes.Buffer)
	s.Report(buf)
	out := buf.String()

	aIdx := strings.Index(out, "apps/a.yaml")
	zIdx := strings.Index(out, "apps/z.yaml")
	if aIdx < 0 || zIdx < 0 || aIdx > zIdx {
		t.Fatalf("expected a.yaml before z.yaml in report; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rollout/conflict/...`
Expected: FAIL with "no Go files" or "undefined" errors.

- [ ] **Step 3: Implement the package**

Create `rollout/conflict/conflict.go`:

```go
package conflict

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

// Reason describes why a file is in conflict.
type Reason int

const (
	// ReasonUnmarked means the file exists in targetPath but has no monotool
	// marker — it was not last written by monotool.
	ReasonUnmarked Reason = iota
	// ReasonHashMismatch means the file has a marker but its current body's
	// hash does not match the marker hash — it was edited since monotool last
	// wrote it.
	ReasonHashMismatch
)

func (r Reason) String() string {
	switch r {
	case ReasonUnmarked:
		return "unmarked"
	case ReasonHashMismatch:
		return "hash-mismatch"
	default:
		return "unknown"
	}
}

// Conflict is a single in-flight conflict detected during a rollout.
type Conflict struct {
	Path   string
	Reason Reason
}

// Set accumulates conflicts across the rollout flow.
type Set struct {
	items []Conflict
}

// New returns an empty Set.
func New() *Set { return &Set{} }

// Add appends a conflict.
func (s *Set) Add(path string, reason Reason) {
	s.items = append(s.items, Conflict{Path: path, Reason: reason})
}

// Empty reports whether the set has zero conflicts.
func (s *Set) Empty() bool { return len(s.items) == 0 }

// ErrConflicts is the sentinel returned by Err when conflicts were collected.
var ErrConflicts = errors.New("rollout aborted: file ownership conflicts")

// Err returns ErrConflicts when the set is non-empty, otherwise nil.
func (s *Set) Err() error {
	if s.Empty() {
		return nil
	}
	return ErrConflicts
}

// Report writes a human-readable conflict report to w, grouped by reason with
// paths sorted within each group. Reasons appear in fixed order: unmarked,
// then hash-mismatch.
func (s *Set) Report(w io.Writer) {
	byReason := map[Reason][]string{}
	for _, c := range s.items {
		byReason[c.Reason] = append(byReason[c.Reason], c.Path)
	}

	fmt.Fprintf(w, "rollout aborted: %d conflict(s) detected\n\n", len(s.items))

	for _, r := range []Reason{ReasonUnmarked, ReasonHashMismatch} {
		paths := byReason[r]
		if len(paths) == 0 {
			continue
		}
		sort.Strings(paths)
		switch r {
		case ReasonUnmarked:
			fmt.Fprintln(w, "unmarked (not owned by monotool):")
		case ReasonHashMismatch:
			fmt.Fprintln(w, "hash-mismatch (edited since last rollout):")
		}
		for _, p := range paths {
			fmt.Fprintf(w, "  %s\n", p)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "re-run with --force to overwrite, or resolve manually and commit before retrying.")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rollout/conflict/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add rollout/conflict/conflict.go rollout/conflict/conflict_test.go
git commit -m "conflict: add Set type and Report formatting"
```

---

## Task 7: Rewrite `generateManifests` to be marker-aware

**Files:**
- Modify: `rollout/rollout.go`
- Create: `rollout/rollout_test.go`

This task changes only the *write* half of the rollout. Pruning is still the old `removeOldManifests` for now; that's Task 8. We also extract `generateManifests` from its closure into a top-level function so it's testable.

The closure currently has signature `generate func(dir string) error`. After this task it will be `func(dir string) (written []string, conflicts *conflict.Set, err error)`. Pruning still happens through the old closure-internal logic at this point — we'll integrate the new prune in Task 8.

- [ ] **Step 1: Write the failing test**

Create `rollout/rollout_test.go`:

```go
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
```

Note: `conflicts.Items()` returns the slice of conflicts. We'll add it to the `conflict` package next.

- [ ] **Step 2: Add `Items()` accessor to the `conflict` package**

Append to `rollout/conflict/conflict.go`:

```go
// Items returns a copy of the collected conflicts.
func (s *Set) Items() []Conflict {
	out := make([]Conflict, len(s.items))
	copy(out, s.items)
	return out
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./rollout/...`
Expected: FAIL with "undefined: GenerateManifests" / "undefined: GenerateOpts".

- [ ] **Step 4: Rewrite `rollout/rollout.go` to expose `GenerateManifests`**

Replace the entire contents of `rollout/rollout.go` with:

```go
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
```

- [ ] **Step 5: Run tests**

Run: `go test ./rollout/...`
Expected: PASS for `ownership`, `conflict`, and the new `rollout` tests.

Note: this step changes the signature of `Rollout.RollOut` (added `force bool`) and the `generate` closure passed to backends. The `gitea`/`github` `RollOut` methods still expect `func(dir string) error`. Compilation will fail; that's Task 9's job. To keep this commit green, also do the next step.

- [ ] **Step 6: Temporarily adapt gitea/github call sites**

In `rollout/gitea/roll_out_to_gitea.go`, change the `RollOut` signature and `generate` call from:

```go
func (g *GiteaRollout) RollOut(ctx context.Context, message string, generate func(dir string) error) error {
```

to:

```go
func (g *GiteaRollout) RollOut(ctx context.Context, message string, generate func(dir string) (added, removed []string, err error)) error {
```

And replace the `err = generate(td)` block with:

```go
_, _, err = generate(td)
if err != nil {
	return fmt.Errorf("could not generate manifests: %w", err)
}
```

Make the same edits to `rollout/github/roll_out_to_github.go`. Keep `gitops.AddFiles(ctx, td)` in place; Task 9 will replace it.

In `command/rollout/command.go`, update the call site:

```go
err = r.RollOut(ctx, cfg.ProjectRoot, values, message, false)
```

(temporary `false` until Task 10 adds the flag).

- [ ] **Step 7: Build and test the whole project**

Run: `go build ./...`
Expected: success.

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add rollout/rollout.go rollout/rollout_test.go rollout/conflict/conflict.go rollout/gitea/roll_out_to_gitea.go rollout/github/roll_out_to_github.go command/rollout/command.go
git commit -m "rollout: marker-aware GenerateManifests with conflict detection"
```

---

## Task 8: Replace `removeOldManifests` with marker-aware prune

**Files:**
- Modify: `rollout/rollout.go`
- Modify: `rollout/rollout_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `rollout/rollout_test.go`:

```go
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
		WorkDir:     workDir,
		TargetPath:  "apps/staging",
		DesiredAbs:  desired,
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
```

Add to the imports of `rollout_test.go`:

```go
"github.com/draganm/monotool/rollout/ownership"
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./rollout/...`
Expected: FAIL with "undefined: Prune" / "undefined: PruneOpts".

- [ ] **Step 3: Implement `Prune`**

Append to `rollout/rollout.go` (and remove the placeholder `_ = workDir` line + the comment in the closure — that wiring is now real):

```go
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

	type owned struct {
		path string
		rel  string
	}
	var ownedFiles []owned
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

		// Orphan sidecar (JSON sidecar whose JSON doesn't exist) → cruft.
		if filepath.Ext(p) == ownership.SidecarExt {
			jsonPath := p[:len(p)-len(ownership.SidecarExt)]
			if _, err := os.Stat(jsonPath); errors.Is(err, os.ErrNotExist) {
				orphanSidecars = append(orphanSidecars, p)
			}
			return nil
		}

		// Sidecars are handled implicitly by ownership.Remove when we delete
		// their JSONs; don't visit them as primary files.
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
		ownedFiles = append(ownedFiles, owned{path: p, rel: rel})
		return nil
	})
	if err != nil {
		return nil, conflicts, fmt.Errorf("walk %s: %w", root, err)
	}

	for _, o := range ownedFiles {
		if _, keep := opts.DesiredAbs[o.path]; keep {
			continue
		}

		st, err := ownership.Status(o.path)
		if err != nil {
			return removed, conflicts, err
		}
		if !st.Matches {
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
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Descend deepest-first.
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
```

- [ ] **Step 4: Wire Prune into the rollout closure**

Replace the `generate` closure in `Rollout.RollOut` with:

```go
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
		all.Report(os.Stderr)
		if !force {
			return nil, nil, all.Err()
		}
	}

	return written, removed, nil
}
```

And add a helper at the bottom of `rollout.go`:

```go
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
```

- [ ] **Step 5: Run tests**

Run: `go test ./rollout/...`
Expected: PASS.

- [ ] **Step 6: Build the whole project**

Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Commit**

```bash
git add rollout/rollout.go rollout/rollout_test.go
git commit -m "rollout: marker-aware Prune replacing removeOldManifests"
```

---

## Task 9: Add `StageChanges` to gitops, remove `AddFiles`

**Files:**
- Modify: `rollout/gitops/operations.go`

- [ ] **Step 1: Add `StageChanges`**

Append to `rollout/gitops/operations.go`:

```go
// StageChanges runs `git add` for every added path and `git rm` for every
// removed path. Paths must be relative to dir or absolute paths inside it. No
// other paths are staged.
func StageChanges(ctx context.Context, dir string, added, removed []string) error {
	if len(added) > 0 {
		args := append([]string{"add", "--"}, added...)
		cmd := exec.CommandContext(ctx, "git", args...)
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git add failed: %w\n%s", err, out.String())
		}
	}
	if len(removed) > 0 {
		args := append([]string{"rm", "--quiet", "--"}, removed...)
		cmd := exec.CommandContext(ctx, "git", args...)
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git rm failed: %w\n%s", err, out.String())
		}
	}
	return nil
}

// HasStagedChanges reports whether any changes are staged in dir.
func HasStagedChanges(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--name-only")
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git diff --cached failed: %w\n%s", err, out.String())
	}
	return out.Len() > 0, nil
}
```

- [ ] **Step 2: Remove `AddFiles`**

Delete the `AddFiles` function from `rollout/gitops/operations.go`.

- [ ] **Step 3: Verify nothing else references `AddFiles`**

Run: `grep -rn "gitops.AddFiles" .`
Expected: no output (or matches only inside `roll_out_to_*.go`, which Task 10 will fix).

- [ ] **Step 4: Commit**

(The gitea/github files won't compile yet — that's the next task. To avoid a broken HEAD, stage but wait to commit until Task 10 Step 5.)

---

## Task 10: Wire `StageChanges` into gitea + github backends

**Files:**
- Modify: `rollout/gitea/roll_out_to_gitea.go`
- Modify: `rollout/github/roll_out_to_github.go`

- [ ] **Step 1: Update gitea**

Replace the `RollOut` method in `rollout/gitea/roll_out_to_gitea.go`:

```go
func (g *GiteaRollout) RollOut(ctx context.Context, message string, generate func(dir string) (added, removed []string, err error)) error {
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}
	defer os.RemoveAll(td)

	if err := gitops.CloneRepo(ctx, g.RepoURL, td); err != nil {
		return err
	}

	commitTime := time.Now().Format("2006-01-02-15-04-05")
	branchName := fmt.Sprintf("rollout-%s", commitTime)
	if err := gitops.CreateBranch(ctx, td, branchName); err != nil {
		return err
	}

	added, removed, err := generate(td)
	if err != nil {
		return fmt.Errorf("could not generate manifests: %w", err)
	}

	if err := gitops.StageChanges(ctx, td, added, removed); err != nil {
		return fmt.Errorf("could not stage changes: %w", err)
	}

	hasChanges, err := gitops.HasStagedChanges(ctx, td)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Println("no changes to roll out")
		return nil
	}

	commitMessage := fmt.Sprintf("rollout %s\n\n%s", commitTime, message)
	if err := gitops.CreateCommit(ctx, td, commitMessage); err != nil {
		return fmt.Errorf("could not create commit: %w", err)
	}
	if err := gitops.PushToOrigin(ctx, td, branchName); err != nil {
		return fmt.Errorf("could not push: %w", err)
	}

	output, err := createPR(ctx, td, fmt.Sprintf("rollout %s", commitTime), message)
	if err != nil {
		return fmt.Errorf("could not create PR: %w", err)
	}
	fmt.Println(output)
	return nil
}
```

- [ ] **Step 2: Update github**

Apply the same shape of change to `rollout/github/roll_out_to_github.go`. The only differences from gitea are the `Base` field and the `createPR` call signature:

```go
func (g *GitHubRollout) RollOut(ctx context.Context, message string, generate func(dir string) (added, removed []string, err error)) error {
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}
	defer os.RemoveAll(td)

	if err := gitops.CloneRepo(ctx, g.RepoURL, td); err != nil {
		return err
	}

	commitTime := time.Now().Format("2006-01-02-15-04-05")
	branchName := fmt.Sprintf("rollout-%s", commitTime)
	if err := gitops.CreateBranch(ctx, td, branchName); err != nil {
		return err
	}

	added, removed, err := generate(td)
	if err != nil {
		return fmt.Errorf("could not generate manifests: %w", err)
	}

	if err := gitops.StageChanges(ctx, td, added, removed); err != nil {
		return fmt.Errorf("could not stage changes: %w", err)
	}

	hasChanges, err := gitops.HasStagedChanges(ctx, td)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Println("no changes to roll out")
		return nil
	}

	commitMessage := fmt.Sprintf("rollout %s\n\n%s", commitTime, message)
	if err := gitops.CreateCommit(ctx, td, commitMessage); err != nil {
		return fmt.Errorf("could not create commit: %w", err)
	}
	if err := gitops.PushToOrigin(ctx, td, branchName); err != nil {
		return fmt.Errorf("could not push: %w", err)
	}

	output, err := createPR(ctx, td, fmt.Sprintf("rollout %s", commitTime), message, g.Base, branchName)
	if err != nil {
		return fmt.Errorf("could not create PR: %w", err)
	}
	fmt.Println(output)
	return nil
}
```

- [ ] **Step 3: Build the whole project**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit Tasks 9 and 10 together**

```bash
git add rollout/gitops/operations.go rollout/gitea/roll_out_to_gitea.go rollout/github/roll_out_to_github.go
git commit -m "gitops: surgical staging via StageChanges (replaces git add .)"
```

---

## Task 11: Expose `--force` on the CLI

**Files:**
- Modify: `command/rollout/command.go`

- [ ] **Step 1: Add the flag**

In `command/rollout/command.go`, extend the `Flags` slice on the returned `cli.Command`:

```go
Flags: []cli.Flag{
	&cli.StringFlag{
		Name:     "message",
		Aliases:  []string{"m"},
		Usage:    "describe the purpose of the rollout (included in the PR description)",
		Required: true,
	},
	&cli.BoolFlag{
		Name:  "force",
		Usage: "overwrite files in the gitops repo that aren't owned by monotool or have been edited since the last rollout",
	},
},
```

- [ ] **Step 2: Thread the flag into the rollout call**

Replace the existing call to `r.RollOut` with:

```go
err = r.RollOut(ctx, cfg.ProjectRoot, values, message, c.Bool("force"))
```

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Run: `go test ./...`
Expected: both pass.

- [ ] **Step 4: Commit**

```bash
git add command/rollout/command.go
git commit -m "rollout: add --force CLI flag"
```

---

## Task 12: Manual smoke test against a local git repo

This task is human-driven verification; there is no commit at the end. Skip if you've already exercised the flow against a real gitops repo.

- [ ] **Step 1: Build the binary**

Run: `go build -o /tmp/monotool-coexist ./`
Expected: binary at `/tmp/monotool-coexist`.

- [ ] **Step 2: Set up a throwaway local "remote"**

```bash
mkdir -p /tmp/coexist-test
cd /tmp/coexist-test
git init --bare remote.git
git clone remote.git working
cd working
mkdir -p apps/staging
echo "initial: true" > apps/staging/handwritten.yaml
git add . && git commit -m "initial human file"
git push
```

- [ ] **Step 3: Run a rollout pointing at the remote**

Configure a minimal `.monotool/config.yaml` in a project of your choice with a rollout whose `gitea.repoUrl` (or `github.repoUrl`) is `/tmp/coexist-test/remote.git` and `pruneTargets: true`. Run `/tmp/monotool-coexist rollout <name> -m "smoke test"`.

Expected: rollout completes; `handwritten.yaml` remains untouched in the gitops repo after the merge.

- [ ] **Step 4: Tamper with a generated file and re-run without `--force`**

Edit one of the rolled-out YAMLs in the gitops repo (e.g., bump a replica count), commit and push. Re-run the rollout without `--force`. Expected: aborts with a `hash-mismatch` conflict listing your edit.

- [ ] **Step 5: Re-run with `--force`**

Re-run with `--force`. Expected: warning printed, edit overwritten, fresh marker.

- [ ] **Step 6: Clean up the test binary**

```bash
rm /tmp/monotool-coexist
rm -rf /tmp/coexist-test
```

---

## Summary of `--force` semantics

| Situation                         | Default behavior             | With `--force`                              |
|-----------------------------------|------------------------------|---------------------------------------------|
| Target path has unmarked file     | abort, list as `unmarked`    | warn + overwrite                            |
| Target file modified since marker | abort, list as `hash-mismatch` | warn + overwrite (writes a fresh marker)   |
| Prune sees modified owned file    | abort, list as `hash-mismatch` | warn + **leave the file in place** (no delete) |
| Prune sees stale owned file       | delete                       | delete                                       |
| Prune sees unowned file           | leave alone                  | leave alone                                  |

The "leave in place" rule for prune-time hash mismatches is deliberate: `--force` lets you overwrite a file you intend to regenerate, but it never silently deletes a file a human has edited.
