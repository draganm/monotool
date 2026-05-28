# Interactive Confirmation for `--force` Rollouts

**Date:** 2026-05-26
**Status:** Draft
**Depends on:** [2026-05-25-gitops-coexistence-design.md](./2026-05-25-gitops-coexistence-design.md)

## Problem

Today `--force` is binary: pass it and monotool silently performs every destructive action across the rollout (overwriting unowned files, overwriting hand-edited owned files, deleting stale files). The flag is the operator's only signal of intent. A typo in a template path or an unexpected manifest in the gitops repo can lead to silent destruction the operator never sees until they look at git history.

Operators want fine-grained control: when `--force` is in play, decide each destructive action individually.

## Goal

Under `--force`, prompt the operator to confirm every destructive action one at a time. Allow per-file approve/skip and a global abort. Preserve all other gitops-coexistence guarantees, including the "never delete a hash-mismatched file in prune" safety rule.

## Non-goals

- "yes-to-all" / "no-to-all" shortcuts. Plain y/n/abort only.
- Reading inputs from a config file or environment variable. The prompt is interactive only.
- Replacing the existing non-interactive `--force` for CI. Non-TTY `--force` becomes an error (see Non-TTY behavior below); CI workflows need a different mechanism if they want forced rollouts, and that's out of scope for this design.
- Prompting under non-`--force` rollouts. Without `--force`, conflicts abort as today — there's nothing destructive to confirm.

## Design

### The Confirmer

A single function type expresses the prompt contract:

```go
// Confirmer asks the user whether to perform a destructive action.
// Returns proceed=true to perform the action, proceed=false to skip just
// this one, or a non-nil error to abort the rollout entirely. A nil
// Confirmer means "no prompting, proceed unconditionally" (preserves the
// pre-prompt behavior for tests).
type Confirmer func(action, path string) (proceed bool, err error)
```

`action` is a short label embedded in the prompt text. The four values used by the rollout flow are:

| action               | When it's prompted                                          |
|----------------------|-------------------------------------------------------------|
| `overwrite unmarked` | GenerateManifests is about to write over a file with no marker |
| `overwrite edited`   | GenerateManifests is about to write over a hash-mismatched owned file |
| `delete stale`       | Prune is about to delete a stale owned file (hash matches)  |
| `remove orphan sidecar` | Prune is about to delete a `.monotool` sidecar whose JSON sibling is gone |

`path` is the gitops-repo-relative path of the file the action targets — the same form used in conflict reports.

### Sentinel: `ErrAborted`

```go
var ErrAborted = errors.New("rollout aborted by user")
```

Returned by `Confirmer` when the operator picks the abort option. The rollout closure surfaces it up; the CLI command catches it and exits cleanly with a non-zero status and a one-line stderr message ("rollout aborted by user"), without a Go-style error wrap chain.

### Where the Confirmer plugs in

Two new fields:

- `GenerateOpts.Confirm Confirmer` — checked inside `GenerateManifests` before each conflict-overwrite write.
- `PruneOpts.Confirm Confirmer` — checked inside `Prune` before each stale-owned deletion and before each orphan-sidecar removal.

Both call the Confirmer immediately before performing the destructive action. The result drives behavior:

- `proceed=true, err=nil` → perform the action, append the resulting path(s) to `written` / `removed`.
- `proceed=false, err=nil` → skip the action, do not append paths. The file remains in the working tree untouched, won't be staged, won't appear in the commit.
- `err != nil` → propagate immediately. `GenerateManifests` and `Prune` return `(written-so-far, conflicts, err)` and `(removed-so-far, conflicts, err)` respectively. The rollout closure surfaces the error and skips StageChanges/commit/push entirely.

For non-conflict paths (clean writes, unowned-file no-ops), the Confirmer is **not** called. Only the four prompt-worthy actions above hit it.

### CLI wiring

In `command/rollout/command.go`, when `--force` is set:

1. Check stdin is a terminal:

   ```go
   if !term.IsTerminal(int(os.Stdin.Fd())) {
       return errors.New("--force requires an interactive terminal")
   }
   ```

   (Uses `golang.org/x/term`, which is already in the module's indirect dependencies.)

2. Construct a `confirm.TTYConfirmer(os.Stdin, os.Stderr)` and pass it through `RollOut`.

3. After `RollOut` returns, if the error is (or wraps) `confirm.ErrAborted`, print `"rollout aborted by user"` to stderr and exit non-zero — but with a clean message, not the full wrapped chain.

When `--force` is not set, no Confirmer is constructed; conflicts go through the existing abort-with-report path unchanged.

### Threading through RollOut

`Rollout.RollOut` gains one more parameter:

```go
func (r *Rollout) RollOut(
    ctx context.Context,
    projectRoot string,
    values map[string]any,
    message string,
    force bool,
    confirm confirm.Confirmer,
) error
```

The closure constructs `GenerateOpts` and `PruneOpts` with `Confirm: confirm`. Callers pass `nil` when `--force` is off (or in tests that want unconditional behavior).

### TTY prompt UX

A short header before the first prompt summarizes scope:

```
--force review: 5 action(s) to confirm one at a time
```

Then, per action:

```
overwrite edited apps/staging/deploy.yaml? [y/n/a] 
```

Input is read line-by-line from `os.Stdin`. The first non-whitespace character is lowercased; subsequent characters are ignored. Mapping:

- `y` → proceed
- `n` → skip
- `a` → abort
- Anything else (including empty line) → reprint the prompt

EOF on stdin (e.g., Ctrl-D) → abort.

The summary header is printed by the CLI command before invoking `RollOut`, using `len(writeConflicts) + len(pruneConflicts)` — except that information isn't known until inside the closure. Two options:

1. Compute the count up-front via a "dry-run" walk (extra I/O, but produces an accurate header).
2. Skip the upfront count and just emit a one-line `"--force review: confirming each destructive action"` header.

Going with (2). The expense of an extra walk to print a number isn't worth it, and the operator sees the count implicitly from the conflict warning that already prints before prompting begins.

### `confirm` package layout

A small new package `rollout/confirm` contains:

- `type Confirmer func(action, path string) (bool, error)`
- `var ErrAborted = errors.New("rollout aborted by user")`
- `func TTYConfirmer(in io.Reader, out io.Writer) Confirmer` — the stdin-backed implementation. Takes `io.Reader`/`io.Writer` so tests inject a `strings.Reader` and `bytes.Buffer`.

`GenerateOpts.Confirm` and `PruneOpts.Confirm` are typed `confirm.Confirmer`. A nil value means "no prompting, proceed unconditionally" — tests that want to skip prompting just leave the field zero-valued.

### Interaction with the existing Warn output

Today, when `--force` succeeds, the rollout closure calls `conflicts.Warn(os.Stderr)` after the destructive actions complete. With prompting, this becomes redundant — each prompt is its own surface of the conflict. The rollout closure should **suppress the Warn output when a Confirmer is in use**, because every approval was already an explicit user choice.

Concretely: in the closure,

```go
if !all.Empty() {
    if force {
        if confirm == nil {
            all.Warn(os.Stderr)  // legacy: --force with no prompting (e.g., a future --yes)
        }
        // else: per-prompt acknowledgement already happened
    } else {
        all.Report(os.Stderr)
        return nil, nil, all.Err()
    }
}
```

### Edge cases

- **Confirmer present but `force=false`.** Not a real path: the CLI only constructs a Confirmer when `--force` is set. If a caller wires it up manually it's harmless — `GenerateManifests` and `Prune` only invoke the Confirmer at conflict sites, and without `--force` they hit the conflict-abort path before reaching any destructive action.
- **Operator skips an overwrite of a JSON sidecar's owner JSON.** Skipping the JSON write also skips writing the sidecar. The existing sidecar (if any) keeps its old hash, which now mismatches — next rollout will flag it as hash-mismatch. This is consistent and expected: skipping means "leave it alone."
- **Operator skips a deletion that monotool's templates have moved.** The stale file remains. Next rollout sees it as still-owned-but-not-in-templates and offers it for deletion again.
- **Operator aborts mid-stream.** Some files may have been written or deleted before the abort. Those changes exist in the working tree of the throwaway clone — they never reach the remote because StageChanges/commit/push are skipped on error. This is consistent with the existing "abort discards local work" guarantee.

## Architecture summary

```
       ┌──────────────────┐
CLI ── │ build Confirmer  │── TTYConfirmer(stdin, stderr)
       │ (only on --force)│
       └────────┬─────────┘
                ▼
       ┌──────────────────────────────┐
       │ Rollout.RollOut(... confirm) │
       └────────┬─────────────────────┘
                │
                ▼
       ┌────────────────────────────────────────┐
       │ generate closure:                      │
       │   GenerateManifests(opts.Confirm=...)  │
       │   Prune(opts.Confirm=...)              │
       │   on confirm-skip: omit path           │
       │   on confirm-error: propagate          │
       └────────┬───────────────────────────────┘
                ▼
       StageChanges (only paths the user said yes to)
                ▼
       commit + push
```

## Error handling

- I/O errors from `Confirmer` (broken stdin pipe etc.) propagate as the abort path. Don't try to recover.
- `ErrAborted` is the documented abort sentinel. Callers check `errors.Is(err, confirm.ErrAborted)` to render the friendly message.
- All other errors retain today's wrapping behavior.

## Testing

`rollout/confirm/confirm_test.go`:

- `TestTTYConfirmerYes` — input `"y\n"` → `(true, nil)`.
- `TestTTYConfirmerNo` — input `"n\n"` → `(false, nil)`.
- `TestTTYConfirmerAbort` — input `"a\n"` → `(false, ErrAborted)`.
- `TestTTYConfirmerRepromptsOnGarbage` — input `"x\n\nq\n y\n"` (or similar) → eventually `(true, nil)`, with prompt printed multiple times to the buffer.
- `TestTTYConfirmerEOFAborts` — empty input → `(false, ErrAborted)`.
- `TestTTYConfirmerPromptText` — verifies the prompt buffer contains `"overwrite edited apps/foo/x.yaml? [y/n/a]"` (action and path included).

`rollout/rollout_test.go` additions:

- `TestGenerateManifestsConfirmYes` — Confirmer returns `(true, nil)`; verify overwrite happens and `written` contains the path.
- `TestGenerateManifestsConfirmNo` — Confirmer returns `(false, nil)`; verify the file is left untouched and `written` does NOT contain the path.
- `TestGenerateManifestsConfirmAbort` — Confirmer returns `(false, ErrAborted)`; verify the error propagates and no further writes happen.
- `TestPruneConfirmYes` / `TestPruneConfirmNo` / `TestPruneConfirmAbort` — analogous for the deletion path.
- `TestPruneOrphanSidecarConfirmNo` — Confirmer returns `(false, nil)` for an orphan sidecar; verify the sidecar remains on disk.

CLI behavior (`command/rollout/command.go`): no new unit tests. Manual verification covers the TTY detection and ErrAborted handling.

## Migration

No data migration. Existing rollouts work unchanged. The first user who runs `--force` in their next rollout will see prompts; that's the new normal.

If someone was relying on `--force` in a non-interactive environment, their first run will fail with the new error message. Document this in the README under the rollout command.

## Open questions

None blocking. The Confirmer interface is intentionally narrow so a future `--yes` flag (skip prompts, proceed unconditionally) could plug in by passing `nil` instead of a TTY Confirmer — but that's a separate spec if anyone needs it.
