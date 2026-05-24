# GitOps Repo Coexistence: File-Level Ownership Markers

**Date:** 2026-05-25
**Status:** Draft

## Problem

Today, `monotool rollout` assumes it is the sole writer of files under each rollout's `targetPath` in the gitops repo. Three behaviors break that assumption when humans or other tools (ArgoCD writebacks, sealed-secrets operators, image updaters, etc.) also commit there:

1. **Same-file overwrite.** `generateManifests` opens every target path with `os.O_TRUNC`, silently clobbering any hand edits a human made between rollouts.
2. **Directory-level prune.** With `pruneTargets: true`, `rollout.go` calls `os.RemoveAll` on whole subdirectories of `targetPath` before regenerating, wiping any non-monotool files committed there.
3. **Broad staging.** `gitops.AddFiles` runs `git add .`, so unrelated changes in the working tree (left by another tool that wrote during the clone window, or a human commit not yet pushed) get rolled into monotool's commit.

The result: anyone sharing the gitops repo with monotool risks losing their work without warning.

## Goal

Let monotool and other writers co-exist in the same gitops repo without monotool destroying their files. When monotool *would* destroy something it does not own, it must stop and surface the conflict to a human rather than continuing silently.

## Non-goals

- Merging hand edits back into templates. If a human edits a generated file, monotool's job is to detect the divergence and refuse to overwrite — not to reconcile.
- Multi-writer locking. Monotool still assumes it has the only in-flight rollout for a given `targetPath` at a time. Cross-rollout coordination is out of scope.
- Retroactive ownership of files monotool generated before this change ships. The first post-upgrade rollout will treat all existing files as unowned and require `--force` (see Migration).

## Design

### File-level ownership markers

Every file monotool writes carries a marker recording the SHA-256 of its own body. The marker proves "monotool last wrote this file, and here's what it wrote." If the file's current body hashes to the marker's value, no one has touched it since.

**YAML files** (`.yaml`, `.yml`): the marker is a comment on the **first line** of the file:

```yaml
# monotool-hash: 9f3c1a...e2b
apiVersion: apps/v1
kind: Deployment
...
```

**JSON files** (`.json`): the marker lives in a **sidecar file** next to the JSON. For `foo.json`, the sidecar is `foo.json.monotool` and contains the hash as a single line of text. The JSON file itself is left untouched.

**Hash computation:** SHA-256 over the file body **excluding the marker line** (YAML) or over the full JSON body (JSON, since its marker is external). Hashing the post-marker body — not the whole file — guarantees that re-running with identical inputs produces a byte-identical file with a byte-identical marker.

### Rollout flow

All work happens in the throwaway clone of the gitops repo. Disk writes and deletions during the rollout never reach the remote unless monotool decides to commit and push at the end — so an abort mid-flow safely discards everything done locally.

For each template the rollout would write:

1. Resolve the target path inside the gitops repo working tree.
2. If the target path does **not** exist on disk:
   - Write the file with a freshly-computed marker. Done.
3. If the target path **does** exist:
   - Read its marker (header line for YAML, sidecar file for JSON).
   - **No marker present** → file is not owned by monotool. Record a conflict (`unmarked`) and continue to the next file.
   - **Marker present, but `hash(current body) != marker hash`** → a human or tool edited the file after monotool last wrote it. Record a conflict (`hash-mismatch`) and continue.
   - **Marker present and matches** → safe. Overwrite with the new content and a fresh marker.
4. After visiting every template, if any conflicts were recorded:
   - Abort the rollout. Do not commit, do not push.
   - Print the full conflict list to stderr, grouped by reason, with file paths relative to the gitops repo root.
   - Exit non-zero.
5. If `--force` was passed on the CLI, conflicts are demoted to warnings: monotool prints the same list but overwrites anyway.

### Pruning (replaces today's `removeOldManifests`)

Pruning runs after writes succeed, only when `pruneTargets: true`. The new algorithm:

1. Walk `targetPath` recursively.
2. For each regular file found:
   - If it has a monotool marker (header for YAML, matching sidecar for JSON):
     - Recompute its hash. If the hash matches the marker, the file is monotool-owned and untouched.
       - If the file is **not** in the current template set, delete it (and its sidecar, if any). It's a leftover from a previous rollout.
       - If the file **is** in the current template set, leave it alone — it was just (re)written above.
     - If the hash does not match the marker, treat as a `hash-mismatch` conflict — same handling as in the write phase (abort without `--force`, warn with `--force` and leave the file alone).
   - If it has no marker, leave it alone — it isn't ours.
3. After deletion, remove any directories under `targetPath` that became empty as a result, walking bottom-up. Never remove a directory that still contains anything (owned or not).

JSON sidecars are deleted together with their JSON. An orphan sidecar (sidecar exists but the JSON doesn't) is treated as monotool-owned cruft and removed during prune.

### Git staging

`gitops.AddFiles`'s current `git add .` is replaced with explicit, path-by-path staging:

- For every file monotool **wrote** in this rollout: `git add <path>` (plus the sidecar path if applicable).
- For every file monotool **deleted** during prune: `git rm <path>` (plus the sidecar path if applicable).
- Nothing else is staged. If unrelated changes sit in the working tree (e.g., a concurrent writer modified a file outside monotool's path set), they remain unstaged and untouched.

If, after staging, `git status --porcelain` shows no staged changes, monotool skips the commit and push entirely and reports "no changes" — matching today's behavior for empty rollouts.

### CLI surface

- `monotool rollout <name>` — unchanged default behavior, except conflicts now abort.
- `monotool rollout <name> --force` — demote conflicts to warnings and overwrite anyway. Force is a per-invocation flag only; it is **not** exposed as a config setting in `.monotool/config.yaml`, because the safety guarantee depends on overriding being a deliberate act each time.

### Conflict reporting format

On abort, monotool prints to stderr:

```
rollout aborted: 3 conflicts detected

unmarked (not owned by monotool):
  apps/staging/foo/extra-configmap.yaml
  apps/staging/foo/secret.json

hash-mismatch (edited since last rollout):
  apps/staging/foo/deployment.yaml

re-run with --force to overwrite, or resolve manually and commit before retrying.
```

The same list (without the "aborted" header) is printed as a warning when `--force` is used.

## Architecture

Two new packages, plus targeted edits to existing ones.

### New: `rollout/ownership`

Single-purpose package that knows about markers.

- `func ReadMarker(path string) (hash string, found bool, err error)` — reads the marker for a file path, dispatching on extension (YAML header vs JSON sidecar).
- `func WriteMarked(path string, body []byte) error` — writes a file with its marker, again dispatching on extension. Replaces the raw `os.OpenFile` + write in `generateManifests`.
- `func ComputeBodyHash(path string, body []byte) string` — exposed so callers can pre-compute hashes when walking the tree during prune.
- `func StripMarker(body []byte, ext string) []byte` — returns the body that would be hashed (drops the marker comment for YAML, returns input unchanged for JSON).
- Package-internal constants for the marker prefix (`# monotool-hash: `) and sidecar extension (`.monotool`).

Tests live alongside (`ownership_test.go`) and cover: round-trip write/read for YAML and JSON, marker-missing detection, hash-mismatch detection after manual edit, sidecar orphan handling.

### New: `rollout/conflict`

Holds the conflict-collection and reporting logic so it can be reused between the write phase and the prune phase.

- `type Conflict struct { Path string; Reason ConflictReason }`
- `type ConflictReason int` with `ReasonUnmarked` and `ReasonHashMismatch`.
- `type Set struct { ... }` accumulates conflicts and exposes `Add`, `Empty`, `Report(w io.Writer)`, and `Err()` (returns a sentinel error when non-empty).

### Changes to `rollout/rollout.go`

- `generateManifests` no longer opens files directly. Instead, it builds the desired-file map (already does this for `templates`), then for each entry calls into a new helper that performs the existence check, marker check, and `ownership.WriteMarked` call. The helper returns `(written bool, conflict *Conflict)`.
- `removeOldManifests` is gone. In its place, a new `prune` function walks `targetPath`, consults the desired-file map, and uses `ownership.ReadMarker` + `ComputeBodyHash` to decide which files to delete.
- The `Rollout` struct gains no new YAML fields. `PruneTargets` keeps its current semantics; `--force` is a CLI flag threaded through as an argument to `RollOut`.
- `RollOut`'s signature gains a `force bool` parameter. Callers in `command/` pass the CLI flag value through.

### Changes to `rollout/gitops/operations.go`

- `AddFiles(ctx, dir)` is replaced (or augmented) with `StageChanges(ctx, dir string, added []string, removed []string) error`, which runs `git add` / `git rm` on the explicit paths. The existing `AddFiles` may be removed entirely if no other caller exists.
- The gitea and github rollout wrappers in `rollout/gitea/` and `rollout/github/` are updated to call `StageChanges` with the lists returned from `generateManifests` and `prune`.

### Changes to `command/`

- The `rollout` CLI command gains a `--force` boolean flag.
- Flag value is passed into `Rollout.RollOut`.

## Data flow

```
              ┌────────────────────────────┐
templates ──▶ │ generateManifests          │
              │  for each template:        │
              │   read marker on disk      │
              │   if conflict → record     │
              │   else        → WriteMarked│
              └─────────────┬──────────────┘
                            │ (writtenPaths, conflictSet)
                            ▼
                  ┌──────────────────┐
                  │ prune (optional) │
                  │  walk targetPath │
                  │  delete orphans  │
                  │  record conflicts│
                  └─────────┬────────┘
                            │ (removedPaths, conflictSet)
                            ▼
            ┌────────────────────────────────┐
            │ conflictSet.Empty() && !force? │
            │   yes → abort, print report    │
            │   no  → continue (warn if any) │
            └────────────────┬───────────────┘
                             ▼
                ┌────────────────────────┐
                │ StageChanges           │
                │  git add writtenPaths  │
                │  git rm removedPaths   │
                └───────────┬────────────┘
                            ▼
                     commit + push
```

## Error handling

- I/O errors (read marker, write file, walk dir) propagate as wrapped errors and abort the rollout immediately — they are not conflicts.
- Malformed markers (e.g., a YAML file starts with `# monotool-hash:` but the value isn't hex) are treated as `hash-mismatch`. Better to flag suspiciously-shaped markers than to ignore them.
- Sidecar I/O errors when the JSON itself reads cleanly are propagated, not converted to conflicts — a broken sidecar suggests a deeper problem than a co-committer.

## Testing

Unit tests in each new package, plus integration tests in `rollout/` that exercise the full flow against a temporary working tree:

- Round-trip: write a YAML and a JSON, read back markers, hash matches.
- First-write into empty `targetPath`: succeeds, all files marked.
- Re-rollout with no changes: hashes match, no conflicts, no commits (empty diff).
- Re-rollout after a hand edit to a YAML: aborts with `hash-mismatch`. With `--force`: warning printed, file overwritten.
- Re-rollout with an extra co-committed file in `targetPath`: file survives. Prune does not delete it.
- Re-rollout where a previously-generated YAML is no longer in the templates: prune deletes it. Same scenario but file was hand-edited after monotool last wrote it: prune aborts (or warns + leaves alone, with `--force`).
- JSON sidecar lifecycle: created on first write, updated on overwrite, deleted on prune, orphaned sidecar removed.
- `git add` granularity: an unrelated file in the working tree is **not** staged.

## Migration

Existing rollouts have no markers on already-generated files. The first run after upgrading will see every file as `unmarked` and abort.

Two paths forward, chosen by the operator:

1. **Run once with `--force`.** Every file gets re-written with a fresh marker. Subsequent rollouts return to the normal safe mode.
2. **Add a one-shot `monotool adopt <rollout>` command** that walks `targetPath`, computes hashes for all files matching the rollout's template set, and writes markers in place without touching the body. The same desired-file mapping logic from `generateManifests` is reused — this command is essentially `generateManifests` with "write marker, keep body" instead of "write template, write marker."

`adopt` is the better operator experience but adds CLI surface. Recommend shipping with `--force` only and adding `adopt` if migration friction is real.

## Open questions

None blocking. Marker syntax (`# monotool-hash: `) and sidecar extension (`.monotool`) are bikesheddable but unimportant — picked for readability and grep-ability.
