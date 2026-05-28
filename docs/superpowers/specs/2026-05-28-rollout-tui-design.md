# Rollout TUI — Design

## Problem

`monotool rollout` currently uses `gosuri/uiprogress` to display one progress bar per image. The progress bar shows only a coarse state label (`initializing` → `building image` → `pushing image` → `done`). Live output from `go generate`, `docker buildx build`, and `docker image push` is captured to a `bytes.Buffer` and shown **only when a step fails**. While a build runs, the user has no insight into what's happening; long-running steps appear stuck.

## Goal

Replace the progress-bar UI with a TUI that lets the user:

1. See the list of all images being processed, with state + elapsed time.
2. Select an image and view its live, streamed output (akin to a tmux pane).
3. Scroll through that output, including after the build completes or fails.

## Scope (in)

- A new `ui/` package implementing a Bubble Tea TUI.
- Refactor of `docker.BuildGoMod`, `docker.BuildDockerfile`, `docker.Push`, and `image.Image.Build` to accept an `io.Writer` for streamed output instead of capturing internally to a `bytes.Buffer`.
- Wiring the rollout command to drive the TUI.
- Non-TTY fallback (CI): plain prefixed line logging.
- Auto-exit on success; stay open on failure until user dismisses.
- Cancellation: Ctrl+C cancels the context and all subprocesses.

## Scope (out)

- Splitting stdout and stderr — current code merges them, we keep that.
- Per-image build logs persisted to disk.
- Mouse support, theming, configuration of key bindings.
- Changing rollout flow itself (templates, gitea/github push) — TUI covers only the build/push phase.

## Architecture

### Dependency changes

Added (direct):

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`
- `github.com/mattn/go-isatty` (already indirect; promote to direct)

Removed:

- `github.com/gosuri/uiprogress`
- `github.com/gosuri/uilive` (transitively pulled by uiprogress)

### Function-signature changes

All build/push functions become writer-driven. Callers supply where the output goes; failure messages no longer embed the full output (the caller has it via the writer).

```go
// docker package
func BuildGoMod(ctx context.Context, mainPackagePath, imageName, platform string, out io.Writer) error
func BuildDockerfile(ctx context.Context, contextDir, dockerfilePath, imageName, platform string, out io.Writer) error
func Push(ctx context.Context, image string, out io.Writer) error

// image package
func (i *Image) Build(ctx context.Context, projectRoot string, out io.Writer) error
```

`error` returned from these functions wraps `cmd.Run()` directly (e.g. `fmt.Errorf("docker build failed: %w", err)`). The output is already in the writer; the caller decides what tail to surface to the user.

`go generate ./...` output is included in the same writer ahead of the docker build output.

### `ui/` package

- `ui.Model` — Bubble Tea root model.
  - `items []*ImageItem` (one per `cfg.Images` entry).
  - `list list.Model` (bubbles/list, left pane).
  - `viewport viewport.Model` (bubbles/viewport, right pane).
  - `selected int`.
  - `width, height int`.
  - `done bool`, `failed bool`.
- `ui.ImageItem`:
  - `Name string` (config key)
  - `ImageName string` (`repo:tag`)
  - `State string` (`waiting`, `checking remote`, `building image`, `pushing image`, `done`, `already pushed`, `failed`, `cancelled`)
  - `Started time.Time` (zero until first state update)
  - `Finished time.Time` (zero until terminal state)
  - `Output *ringbuffer.Buffer` (bounded line buffer)
  - `Err error`
- `ui.Program` — wraps `tea.Program` and exposes:
  - `Writer(name string) io.Writer` — returns a `LineWriter` that publishes `ImageOutputMsg`.
  - `SetState(name, state string)`.
  - `SetImageName(name, imageName string)`.
  - `Finish(name string, err error)`.
  - `FinishAll()`.
  - `Run(ctx context.Context) error` — blocking; returns when user dismisses (or auto on success).

### Bubble Tea messages

- `imageStateMsg{Name, State string}`
- `imageNameMsg{Name, ImageName string}`
- `imageOutputMsg{Name string, Line string}`
- `imageDoneMsg{Name string, Err error}`
- `allDoneMsg{}` — triggers auto-quit if no failures.
- `tickMsg` — every 1s, refreshes elapsed-time display.
- `tea.KeyMsg`, `tea.WindowSizeMsg` — standard Bubble Tea.

### Ring buffer

`ui/internal/ringbuffer` — a fixed-capacity (default 2000 lines) line buffer. Append O(1). Iterate in order. Thread-safe (the model only mutates from the Update loop, but the writer also appends — we route appends through `tea.Msg` so the buffer itself can be single-threaded).

Decision: the ring buffer lives inside the model and is mutated only by `Update`. `LineWriter` sends `imageOutputMsg`. This keeps Bubble Tea's "model is updated only via messages" invariant.

### LineWriter

`ui/linewriter.go`:

```go
type LineWriter struct {
    program *tea.Program
    name    string
    buf     []byte   // partial line buffer, flushed on '\n'
}

func (w *LineWriter) Write(p []byte) (int, error) { ... }
```

Handles:

- Partial lines (no trailing `\n`) — buffered until next write.
- `\r`-only carriage returns from `docker buildx --progress=plain` — treated as line terminators so progress lines don't accumulate.
- `\x1b[...` ANSI sequences — stripped before display (lipgloss does its own styling; raw ANSI from docker would break layout). Use a simple regex-based strip.
- Last line flushed on `Close()`.

### Layout

Two-pane layout via lipgloss:

```
┌─ monotool rollout ───────────────────────────────────────────────────────┐
│ ▸ api          building            00:34   │ #5 0.342 + go test ./...    │
│   worker       pushing             01:12   │ #5 1.230 ok                 │
│   migrator     done                00:08   │ #6 [internal] exporting     │
│   docs         already pushed      00:01   │  ...                        │
│   frontend     failed              00:22   │ ERROR: build failed         │
│                                            │                             │
│                                            │ (scroll: PgUp/PgDn  end: G) │
├────────────────────────────────────────────┴─────────────────────────────┤
│ ↑/↓ navigate · PgUp/PgDn scroll · q quit · ctrl-c cancel    3/5 complete │
└──────────────────────────────────────────────────────────────────────────┘
```

- Left pane: ~40% width; bubbles/list with custom item renderer (name padded, state colored, elapsed `mm:ss`).
- Right pane: viewport bound to the selected item's ring buffer. When the selection changes, viewport content is reset to that item's buffer and scrolled to the bottom.
- Auto-follow: if user has scrolled to the bottom (or hasn't scrolled), new lines auto-scroll. If user scrolled up, position is preserved until they press `G`.

### Key bindings

| Key | Action |
|-----|--------|
| `↑` / `k` | previous image |
| `↓` / `j` | next image |
| `PgUp` / `PgDn` | scroll output |
| `g` | scroll output to top |
| `G` | scroll output to bottom (re-enables auto-follow) |
| `q` | quit (only after all done; ignored while builds in flight) |
| `Ctrl+C` | cancel context, kill subprocesses |

### Auto-exit behavior

When `allDoneMsg` is received:

- If `failed == false`: program sends `tea.Quit` immediately, control returns to rollout command, which proceeds to `r.RollOut(...)`.
- If `failed == true`: program stays open, footer changes to `q quit (failed)`. On `q`, returns and the rollout command returns the aggregated error.

### Cancellation

- `Ctrl+C` calls a stored `cancel` function (the `signal.NotifyContext` cancel is wired in).
- Each in-flight `exec.CommandContext` is killed by the context.
- Each image transitions to state `cancelled` (or its current terminal state if already finished).
- Program waits for `allDoneMsg`, then quits with non-zero error.

### Non-TTY fallback

`isatty.IsTerminal(os.Stdout.Fd()) == false` (e.g. CI, piped output):

- Skip Bubble Tea entirely.
- `Program.Writer(name)` returns a writer that prefixes each line with `[name] ` and writes to `os.Stdout`.
- State transitions printed as `[name] state: building image`.
- Errors printed as `[name] FAILED: <err>` with full output already emitted.
- Auto-exit immediately on completion (no `q` prompt).

### Files

```
ui/
├── program.go        # Program type, Run/Send helpers, isatty fallback
├── model.go          # tea.Model: Init/Update/View
├── messages.go       # tea.Msg types
├── linewriter.go     # io.Writer that emits imageOutputMsg
├── linewriter_test.go
├── fallback.go       # non-TTY plain-text writer
├── styles.go         # lipgloss styles (state colors, panes, borders)
└── internal/
    └── ringbuffer/
        ├── ringbuffer.go
        └── ringbuffer_test.go
```

### Rollout command refactor

[command/rollout/command.go](command/rollout/command.go) `Action` becomes (sketch):

```go
prog := ui.New(cfg.Images)        // builds items
go func() { prog.Run(ctx) }()     // (or: program.Start() and Wait pattern)

for n, im := range cfg.Images {
    eg.Go(func() error {
        w := prog.Writer(n)
        prog.SetState(n, "checking remote")
        imageName, err := im.DockerImageName(egCtx, cfg.ProjectRoot)
        if err != nil { prog.Finish(n, err); return err }
        prog.SetImageName(n, imageName)
        // ... existing logic, replacing bar.Set/state.Store with prog.SetState(...)
        // ... im.Build(egCtx, cfg.ProjectRoot, w)
        // ... docker.Push(egCtx, imageName, w)
        prog.Finish(n, nil)
        return nil
    })
}

err := eg.Wait()
prog.FinishAll()
// program returns when user dismisses (or auto on success)
if waitErr := prog.Wait(); waitErr != nil { return waitErr }
if err != nil { return fmt.Errorf("could not build images: %w", err) }

// proceed to rollout phase as before
```

The `uiprogress`, `strutil`, `pointerOf`, and `atomic.Pointer[string]` plumbing is deleted.

## Error handling

- A build error → `Program.Finish(name, err)` → state becomes `failed` → image's ring buffer already contains the tail of the failure. Error is also stored on the item so the right pane can append a final `ERROR: <err>` line.
- A context cancellation → state becomes `cancelled`.
- Multiple failures → all surfaced; the rollout command returns a joined error (`errors.Join`).

## Testing

- `ui/internal/ringbuffer`: unit tests for append/iterate/overflow.
- `ui/linewriter_test.go`: tests for partial lines, `\r` handling, ANSI stripping.
- `ui/model_test.go`: `teatest`-based test that feeds a scripted sequence of messages and asserts the rendered frame contains expected substrings (e.g. image name, state label, output snippet).
- Existing `docker/hash_test.go` untouched.

Manual smoke test: run `monotool rollout -m "tui smoke"` on a small monorepo, verify navigation, scrolling, success exit, and that triggering a build failure leaves the TUI open showing the failure output.

## Open questions / decisions made

- **stdout vs stderr**: merged, matching current behavior. Decision.
- **Default ring-buffer size**: 2000 lines. Generous for typical builds; ~200 KB at 100 chars/line.
- **Auto-quit on success**: yes. Decision.
- **`q` to quit during in-flight builds**: ignored. Use Ctrl+C to cancel.
