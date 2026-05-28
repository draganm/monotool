# Interactive `--force` Confirmation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Under `--force`, prompt the operator y/n/abort for every destructive action (overwrites, deletions, orphan-sidecar removals), and require an interactive terminal when `--force` is set.

**Architecture:** A new `rollout/confirm` package defines a `Confirmer` callback (`func(action, path string) (bool, error)`) and a stdin-backed `TTYConfirmer`. `GenerateOpts` and `PruneOpts` gain a `Confirm` field; both invoke it immediately before each destructive action. The closure in `Rollout.RollOut` and the CLI command thread a TTY-backed Confirmer through when `--force` is set; non-TTY `--force` errors out. A sentinel `ErrAborted` is returned when the user picks the abort option, and the CLI prints a friendly message instead of the wrapped error chain.

**Tech Stack:** Go 1.25, stdlib (`bufio`, `errors`, `io`, `strings`) plus `golang.org/x/term` for the TTY check.

**Spec:** [docs/superpowers/specs/2026-05-26-force-interactive-confirm-design.md](../specs/2026-05-26-force-interactive-confirm-design.md)

---

## File Structure

**New files:**
- `rollout/confirm/confirm.go` — `Confirmer` type, `ErrAborted` sentinel, `TTYConfirmer` constructor. Owns the prompt format and all stdin parsing.
- `rollout/confirm/confirm_test.go` — table-driven tests for `TTYConfirmer` against a `strings.Reader` / `bytes.Buffer`.

**Modified files:**
- `rollout/rollout.go`:
  - `GenerateOpts` gains `Confirm confirm.Confirmer`.
  - `GenerateManifests` calls `Confirm` before each overwrite-on-conflict write.
  - `PruneOpts` gains `Confirm confirm.Confirmer`.
  - `Prune` calls `Confirm` before each stale-owned deletion and before each orphan-sidecar removal.
  - `Rollout.RollOut` signature gains a `confirm confirm.Confirmer` parameter and threads it into both opts structs.
  - The closure suppresses `Warn` output when `confirm != nil` (each prompt is already an explicit ack).
- `rollout/rollout_test.go` — adds Confirm-related test cases for both `GenerateManifests` and `Prune`.
- `command/rollout/command.go`:
  - When `--force` is set, check `term.IsTerminal(int(os.Stdin.Fd()))`. Non-TTY → return an error.
  - Construct a `confirm.TTYConfirmer(os.Stdin, os.Stderr)` and pass to `RollOut`.
  - Catch `confirm.ErrAborted` (via `errors.Is`) and surface as `"rollout aborted by user"` rather than the wrapped chain.
- `go.mod`, `go.sum` — `golang.org/x/term` upgraded from indirect to direct.

**File responsibilities:**
- `rollout/confirm` knows only about the prompt UI. It does not know about manifests, rollouts, or files. It exports a `Confirmer` and one implementation.
- `rollout` orchestrates: it constructs the Confirmer's inputs (action label, relative path), invokes the callback, and acts on the result.
- The CLI command owns TTY detection and the friendly error message for `ErrAborted`.

---

## Conventions

- **Action labels** (passed as the `action` argument to `Confirmer`):
  - `"overwrite unmarked"` — GenerateManifests about to write over an unowned file.
  - `"overwrite edited"` — GenerateManifests about to write over a hash-mismatched owned file.
  - `"delete stale"` — Prune about to delete a stale owned file.
  - `"remove orphan sidecar"` — Prune about to remove a `.monotool` whose JSON sibling is gone.
- **Path argument** is gitops-repo-relative (the same form as conflict reports), not absolute.
- **`nil` Confirmer** means "no prompting, proceed unconditionally" — preserves the pre-prompt behavior for tests and for the legacy `--force` path if a future `--yes` flag is added.

---

## Task 1: Create `rollout/confirm` package skeleton

**Files:**
- Create: `rollout/confirm/confirm.go`

- [ ] **Step 1: Create the package file**

```go
package confirm

import (
	"errors"
)

// Confirmer asks the user whether to perform a destructive action.
// Returns proceed=true to perform the action, proceed=false to skip just
// this one, or a non-nil error to abort the rollout entirely. A nil
// Confirmer means "no prompting, proceed unconditionally".
type Confirmer func(action, path string) (proceed bool, err error)

// ErrAborted is returned by a Confirmer when the user picks the abort
// option. Callers should check via errors.Is and render a friendly message
// rather than a wrapped error chain.
var ErrAborted = errors.New("rollout aborted by user")
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./rollout/confirm/...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add rollout/confirm/confirm.go
git commit -m "confirm: add Confirmer type and ErrAborted sentinel"
```

---

## Task 2: Implement `TTYConfirmer` with tests

**Files:**
- Modify: `rollout/confirm/confirm.go`
- Create: `rollout/confirm/confirm_test.go`

- [ ] **Step 1: Write the failing tests**

Create `rollout/confirm/confirm_test.go`:

```go
package confirm

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTTYConfirmerYes(t *testing.T) {
	out := new(bytes.Buffer)
	c := TTYConfirmer(strings.NewReader("y\n"), out)
	proceed, err := c("delete stale", "apps/foo/x.yaml")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !proceed {
		t.Fatal("proceed = false, want true")
	}
}

func TestTTYConfirmerNo(t *testing.T) {
	out := new(bytes.Buffer)
	c := TTYConfirmer(strings.NewReader("n\n"), out)
	proceed, err := c("delete stale", "apps/foo/x.yaml")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if proceed {
		t.Fatal("proceed = true, want false")
	}
}

func TestTTYConfirmerAbort(t *testing.T) {
	out := new(bytes.Buffer)
	c := TTYConfirmer(strings.NewReader("a\n"), out)
	proceed, err := c("delete stale", "apps/foo/x.yaml")
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if proceed {
		t.Fatal("proceed = true, want false")
	}
}

func TestTTYConfirmerRepromptsOnGarbage(t *testing.T) {
	out := new(bytes.Buffer)
	c := TTYConfirmer(strings.NewReader("\nxyz\ny\n"), out)
	proceed, err := c("overwrite edited", "apps/foo/x.yaml")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !proceed {
		t.Fatal("proceed = false, want true")
	}
	prompts := strings.Count(out.String(), "[y/n/a]")
	if prompts < 3 {
		t.Fatalf("expected at least 3 prompts, got %d, output:\n%s", prompts, out.String())
	}
}

func TestTTYConfirmerEOFAborts(t *testing.T) {
	out := new(bytes.Buffer)
	c := TTYConfirmer(strings.NewReader(""), out)
	proceed, err := c("delete stale", "apps/foo/x.yaml")
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
	if proceed {
		t.Fatal("proceed = true, want false")
	}
}

func TestTTYConfirmerPromptText(t *testing.T) {
	out := new(bytes.Buffer)
	c := TTYConfirmer(strings.NewReader("y\n"), out)
	_, _ = c("overwrite edited", "apps/staging/deploy.yaml")
	got := out.String()
	if !strings.Contains(got, "overwrite edited") {
		t.Errorf("prompt missing action label: %q", got)
	}
	if !strings.Contains(got, "apps/staging/deploy.yaml") {
		t.Errorf("prompt missing path: %q", got)
	}
	if !strings.Contains(got, "[y/n/a]") {
		t.Errorf("prompt missing choices: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./rollout/confirm/...`
Expected: FAIL with "undefined: TTYConfirmer".

- [ ] **Step 3: Implement `TTYConfirmer`**

Append to `rollout/confirm/confirm.go`:

```go
import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// TTYConfirmer returns a Confirmer that prompts on out and reads decisions
// from in line-by-line. The first non-whitespace character of each input
// line is lowercased and mapped: 'y' = proceed, 'n' = skip, 'a' = abort.
// Anything else reprints the prompt. EOF on in aborts.
func TTYConfirmer(in io.Reader, out io.Writer) Confirmer {
	br := bufio.NewReader(in)
	return func(action, path string) (bool, error) {
		for {
			fmt.Fprintf(out, "%s %s? [y/n/a] ", action, path)
			line, err := br.ReadString('\n')
			if err == io.EOF && line == "" {
				return false, ErrAborted
			}
			if err != nil && err != io.EOF {
				return false, fmt.Errorf("read confirmation: %w", err)
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			switch strings.ToLower(trimmed)[0] {
			case 'y':
				return true, nil
			case 'n':
				return false, nil
			case 'a':
				return false, ErrAborted
			}
			// otherwise, loop and reprompt
		}
	}
}
```

Merge the import block at the top of the file into a single block. Final imports: `bufio`, `errors`, `fmt`, `io`, `strings`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./rollout/confirm/...`
Expected: PASS for all six tests.

- [ ] **Step 5: Commit**

```bash
git add rollout/confirm/confirm.go rollout/confirm/confirm_test.go
git commit -m "confirm: add TTYConfirmer for y/n/a prompts"
```

---

## Task 3: Add `Confirm` to `GenerateOpts` and wire it into `GenerateManifests`

**Files:**
- Modify: `rollout/rollout.go`
- Modify: `rollout/rollout_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `rollout/rollout_test.go`:

```go
func TestGenerateManifestsConfirmYes(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	})
	workDir := t.TempDir()
	target := filepath.Join(workDir, "apps/staging/deploy.yaml")
	mustWrite(t, target, "kind: HumanlyEdited\n")

	var calls []string
	conf := func(action, path string) (bool, error) {
		calls = append(calls, action+":"+path)
		return true, nil
	}

	written, _, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
		Force:         true,
		Confirm:       conf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "overwrite unmarked:apps/staging/deploy.yaml" {
		t.Fatalf("calls = %v", calls)
	}
	if len(written) != 1 || written[0] != target {
		t.Fatalf("written = %v, want [%s]", written, target)
	}
	body, _ := os.ReadFile(target)
	if !strings.Contains(string(body), "kind: Deployment") {
		t.Fatalf("file not overwritten: %s", body)
	}
}

func TestGenerateManifestsConfirmNo(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	})
	workDir := t.TempDir()
	target := filepath.Join(workDir, "apps/staging/deploy.yaml")
	mustWrite(t, target, "kind: HumanlyEdited\n")

	conf := func(action, path string) (bool, error) {
		return false, nil
	}

	written, _, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
		Force:         true,
		Confirm:       conf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Fatalf("written = %v, want empty", written)
	}
	body, _ := os.ReadFile(target)
	if string(body) != "kind: HumanlyEdited\n" {
		t.Fatalf("file was modified, body = %q", body)
	}
}

func TestGenerateManifestsConfirmAbort(t *testing.T) {
	templatesDir := setupTemplates(t, map[string]string{
		"deploy.yaml": "kind: Deployment\n",
	})
	workDir := t.TempDir()
	mustWrite(t, filepath.Join(workDir, "apps/staging/deploy.yaml"), "kind: HumanlyEdited\n")

	conf := func(action, path string) (bool, error) {
		return false, confirm.ErrAborted
	}

	_, _, err := GenerateManifests(context.Background(), GenerateOpts{
		TemplatesPath: templatesDir,
		WorkDir:       workDir,
		TargetPath:    "apps/staging",
		Values:        map[string]any{},
		Force:         true,
		Confirm:       conf,
	})
	if !errors.Is(err, confirm.ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
}
```

Add to the imports of `rollout_test.go`:

```go
"errors"
"github.com/draganm/monotool/rollout/confirm"
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./rollout/...`
Expected: FAIL with "unknown field Confirm in GenerateOpts" and similar.

- [ ] **Step 3: Add `Confirm` to `GenerateOpts` and wire it into `GenerateManifests`**

In `rollout/rollout.go`:

(a) Add the confirm import to the import block:

```go
"github.com/draganm/monotool/rollout/confirm"
```

(b) Extend `GenerateOpts`:

```go
// GenerateOpts is the input to GenerateManifests. It is exposed (and the
// function is exported) so tests can drive the write phase without needing a
// git remote.
type GenerateOpts struct {
	TemplatesPath string
	WorkDir       string
	TargetPath    string
	Values        map[string]any
	Force         bool
	// Confirm, when non-nil, is invoked before each overwrite-on-conflict
	// write under --force. Returning proceed=false skips the write; returning
	// a non-nil error aborts GenerateManifests.
	Confirm confirm.Confirmer
}
```

(c) Update the conflict-handling block inside `GenerateManifests`:

Replace this block:

```go
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
```

with:

```go
		if st.Exists && (!st.Owned || !st.Matches) {
			reason := conflict.ReasonUnmarked
			action := "overwrite unmarked"
			if st.Owned {
				reason = conflict.ReasonHashMismatch
				action = "overwrite edited"
			}
			conflicts.Add(relPath, reason)
			if !opts.Force {
				continue
			}
			if opts.Confirm != nil {
				proceed, err := opts.Confirm(action, relPath)
				if err != nil {
					return written, conflicts, err
				}
				if !proceed {
					continue
				}
			}
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./rollout/...`
Expected: PASS.

- [ ] **Step 5: Build the whole project**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add rollout/rollout.go rollout/rollout_test.go
git commit -m "rollout: prompt Confirmer before each overwrite under --force"
```

---

## Task 4: Add `Confirm` to `PruneOpts` and wire it into `Prune`

**Files:**
- Modify: `rollout/rollout.go`
- Modify: `rollout/rollout_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `rollout/rollout_test.go`:

```go
func TestPruneConfirmYes(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	stale := filepath.Join(targetDir, "stale.yaml")
	mustWriteMarked(t, stale, "kind: Stale\n")

	var calls []string
	conf := func(action, path string) (bool, error) {
		calls = append(calls, action+":"+path)
		return true, nil
	}

	removed, _, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
		Confirm:    conf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "delete stale:apps/staging/stale.yaml" {
		t.Fatalf("calls = %v", calls)
	}
	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("removed = %v, want [%s]", removed, stale)
	}
}

func TestPruneConfirmNo(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	stale := filepath.Join(targetDir, "stale.yaml")
	mustWriteMarked(t, stale, "kind: Stale\n")

	conf := func(action, path string) (bool, error) {
		return false, nil
	}

	removed, _, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
		Confirm:    conf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want empty", removed)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("stale file was deleted: %v", err)
	}
}

func TestPruneConfirmAbort(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	stale := filepath.Join(targetDir, "stale.yaml")
	mustWriteMarked(t, stale, "kind: Stale\n")

	conf := func(action, path string) (bool, error) {
		return false, confirm.ErrAborted
	}

	_, _, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
		Confirm:    conf,
	})
	if !errors.Is(err, confirm.ErrAborted) {
		t.Fatalf("err = %v, want ErrAborted", err)
	}
}

func TestPruneOrphanSidecarConfirmNo(t *testing.T) {
	workDir := t.TempDir()
	targetDir := filepath.Join(workDir, "apps/staging")
	sidecar := filepath.Join(targetDir, "ghost.json.monotool")
	mustWrite(t, sidecar, "deadbeef\n")

	var calls []string
	conf := func(action, path string) (bool, error) {
		calls = append(calls, action+":"+path)
		return false, nil
	}

	removed, _, err := Prune(context.Background(), PruneOpts{
		WorkDir:    workDir,
		TargetPath: "apps/staging",
		DesiredAbs: map[string]struct{}{},
		Confirm:    conf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "remove orphan sidecar:apps/staging/ghost.json.monotool" {
		t.Fatalf("calls = %v", calls)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want empty", removed)
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar was deleted: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./rollout/...`
Expected: FAIL with "unknown field Confirm in PruneOpts".

- [ ] **Step 3: Add `Confirm` to `PruneOpts` and wire into `Prune`**

In `rollout/rollout.go`:

(a) Extend `PruneOpts`:

```go
// PruneOpts drives Prune. DesiredAbs is the set of absolute paths Prune must
// leave alone (typically: every path GenerateManifests wrote, including
// sidecars).
type PruneOpts struct {
	WorkDir    string
	TargetPath string
	DesiredAbs map[string]struct{}
	Force      bool
	// Confirm, when non-nil, is invoked before each deletion (stale owned
	// file or orphan sidecar). Returning proceed=false skips that deletion;
	// returning a non-nil error aborts Prune.
	Confirm confirm.Confirmer
}
```

(b) In the stale-owned deletion loop, replace:

```go
		if err := ownership.Remove(o.path); err != nil {
			return removed, conflicts, err
		}
		removed = append(removed, o.path)
		if filepath.Ext(o.path) == ".json" {
			removed = append(removed, o.path+ownership.SidecarExt)
		}
```

with (note: `o.rel` is already computed during the walk and stored in the `ownedFile` struct):

```go
		if opts.Confirm != nil {
			proceed, err := opts.Confirm("delete stale", o.rel)
			if err != nil {
				return removed, conflicts, err
			}
			if !proceed {
				continue
			}
		}
		if err := ownership.Remove(o.path); err != nil {
			return removed, conflicts, err
		}
		removed = append(removed, o.path)
		if filepath.Ext(o.path) == ".json" {
			removed = append(removed, o.path+ownership.SidecarExt)
		}
```

(c) In the orphan-sidecar loop, replace:

```go
	for _, p := range orphanSidecars {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, conflicts, err
		}
		removed = append(removed, p)
	}
```

with:

```go
	for _, p := range orphanSidecars {
		if opts.Confirm != nil {
			rel, relErr := filepath.Rel(opts.WorkDir, p)
			if relErr != nil {
				return removed, conflicts, relErr
			}
			proceed, err := opts.Confirm("remove orphan sidecar", rel)
			if err != nil {
				return removed, conflicts, err
			}
			if !proceed {
				continue
			}
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, conflicts, err
		}
		removed = append(removed, p)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./rollout/...`
Expected: PASS.

- [ ] **Step 5: Build the whole project**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add rollout/rollout.go rollout/rollout_test.go
git commit -m "rollout: prompt Confirmer before each Prune deletion under --force"
```

---

## Task 5: Thread Confirmer through `Rollout.RollOut` and suppress Warn when prompting

**Files:**
- Modify: `rollout/rollout.go`
- Modify: `rollout/rollout_test.go`

This task wires the Confirmer through the orchestration layer and removes the now-redundant `Warn` call when prompting is active.

- [ ] **Step 1: Change the `RollOut` signature and closure**

In `rollout/rollout.go`, change the `RollOut` method:

```go
func (r *Rollout) RollOut(ctx context.Context, projectRoot string, values map[string]any, message string, force bool, conf confirm.Confirmer) error {
```

In the closure body, replace this block:

```go
	generate := func(workDir string) (added, removed []string, err error) {
		written, writeConflicts, err := GenerateManifests(ctx, GenerateOpts{
			TemplatesPath: templatesAbs,
			WorkDir:       workDir,
			TargetPath:    r.TargetPath,
			Values:        values,
			Force:         force,
		})
```

with:

```go
	generate := func(workDir string) (added, removed []string, err error) {
		written, writeConflicts, err := GenerateManifests(ctx, GenerateOpts{
			TemplatesPath: templatesAbs,
			WorkDir:       workDir,
			TargetPath:    r.TargetPath,
			Values:        values,
			Force:         force,
			Confirm:       conf,
		})
```

Then in the Prune call, replace:

```go
			pruned, pruneConflicts, err = Prune(ctx, PruneOpts{
				WorkDir:    workDir,
				TargetPath: r.TargetPath,
				DesiredAbs: desired,
				Force:      force,
			})
```

with:

```go
			pruned, pruneConflicts, err = Prune(ctx, PruneOpts{
				WorkDir:    workDir,
				TargetPath: r.TargetPath,
				DesiredAbs: desired,
				Force:      force,
				Confirm:    conf,
			})
```

Finally, replace the Warn/Report block:

```go
		all := mergeConflicts(writeConflicts, pruneConflicts)
		if !all.Empty() {
			if force {
				all.Warn(os.Stderr)
			} else {
				all.Report(os.Stderr)
				return nil, nil, all.Err()
			}
		}
```

with:

```go
		all := mergeConflicts(writeConflicts, pruneConflicts)
		if !all.Empty() {
			switch {
			case !force:
				all.Report(os.Stderr)
				return nil, nil, all.Err()
			case conf == nil:
				// --force without a Confirmer: legacy unconditional behavior
				all.Warn(os.Stderr)
			}
			// --force with a Confirmer: each prompt was its own ack, no extra output
		}
```

- [ ] **Step 2: Update the CLI call site temporarily**

In `command/rollout/command.go`, find the existing call:

```go
err = r.RollOut(ctx, cfg.ProjectRoot, values, message, c.Bool("force"))
```

and change to:

```go
err = r.RollOut(ctx, cfg.ProjectRoot, values, message, c.Bool("force"), nil)
```

The `nil` is a placeholder until Task 6 wires the TTY Confirmer. This keeps the CLI compiling now.

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Run: `go test ./...`
Expected: both pass.

- [ ] **Step 4: Commit**

```bash
git add rollout/rollout.go command/rollout/command.go
git commit -m "rollout: thread Confirmer through RollOut, suppress Warn when prompting"
```

---

## Task 6: Wire TTY Confirmer into the CLI command

**Files:**
- Modify: `command/rollout/command.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add `golang.org/x/term` as a direct dependency**

Run: `go get golang.org/x/term`

This promotes it from indirect to direct and downloads the latest compatible version.

- [ ] **Step 2: Update `command/rollout/command.go`**

Replace the placeholder line:

```go
err = r.RollOut(ctx, cfg.ProjectRoot, values, message, c.Bool("force"), nil)
```

with the TTY-aware version:

```go
var conf confirm.Confirmer
if c.Bool("force") {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("--force requires an interactive terminal")
	}
	conf = confirm.TTYConfirmer(os.Stdin, os.Stderr)
}

err = r.RollOut(ctx, cfg.ProjectRoot, values, message, c.Bool("force"), conf)
if errors.Is(err, confirm.ErrAborted) {
	fmt.Fprintln(os.Stderr, "rollout aborted by user")
	return cli.Exit("", 1)
}
```

Add the required imports to `command/rollout/command.go`:

```go
"errors"
"os"

"github.com/draganm/monotool/rollout/confirm"
"golang.org/x/term"
```

(`os` is likely already imported via the signal package, and `errors` may already be there too — confirm before adding and de-duplicate.)

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Run: `go test ./...`
Expected: both pass.

- [ ] **Step 4: Run go mod tidy to clean up the dependency**

Run: `go mod tidy`
Expected: `go.mod` lists `golang.org/x/term` as a direct dependency (not in the indirect block).

- [ ] **Step 5: Commit**

```bash
git add command/rollout/command.go go.mod go.sum
git commit -m "rollout: wire TTY confirm prompt for --force"
```

---

## Task 7: Smoke-test the prompt flow against a local repo

Optional human-driven verification. Skip if you've already exercised the flow.

- [ ] **Step 1: Build a binary**

Run: `go build -o /tmp/monotool-prompt ./`
Expected: binary at /tmp/monotool-prompt.

- [ ] **Step 2: Set up a local gitops remote with an unrelated file**

```bash
mkdir -p /tmp/prompt-test
cd /tmp/prompt-test
git init --bare remote.git
git clone remote.git working
cd working
mkdir -p apps/staging
echo "kind: HumanFile" > apps/staging/handwritten.yaml
git add . && git commit -m "human file"
git push
```

- [ ] **Step 3: Configure a `.monotool/config.yaml` in any project**

Point its rollout's `repoUrl` at `/tmp/prompt-test/remote.git` with `pruneTargets: true` and `targetPath: apps/staging`. Templates should generate at least one file at the same path as the handwritten file to force a conflict.

- [ ] **Step 4: Run with `--force` interactively**

Run: `/tmp/monotool-prompt rollout <name> --force -m "smoke test"`
Expected: prompt for `overwrite unmarked apps/staging/<file>? [y/n/a]`. Try `y`, `n`, and `a` across multiple runs to verify each path.

- [ ] **Step 5: Run with `--force` non-interactively**

Run: `/tmp/monotool-prompt rollout <name> --force -m "smoke" < /dev/null`
Expected: error `"--force requires an interactive terminal"`.

- [ ] **Step 6: Clean up the test binary and fixtures**

```bash
rm /tmp/monotool-prompt
rm -rf /tmp/prompt-test
```
