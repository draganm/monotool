# Rollout TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the rollout progress bars with a Bubble Tea TUI that shows a list of images on the left and a live, scrollable output viewport for the selected image on the right.

**Architecture:** A new `ui/` package owns the TUI (model + messages + line writer + ring buffer). The `docker` and `image` packages are refactored so build/push functions stream their output to a caller-supplied `io.Writer` instead of buffering internally. The rollout command wires per-image writers from the TUI program to the build steps, drives state transitions, and (on TTY) renders the TUI; on non-TTY it falls back to plain prefixed line logging.

**Tech Stack:** Go, Bubble Tea (`charmbracelet/bubbletea`), Bubbles (`charmbracelet/bubbles` — `list`, `viewport`), Lipgloss (`charmbracelet/lipgloss`), `mattn/go-isatty`.

**Reference:** Design doc at [docs/superpowers/specs/2026-05-28-rollout-tui-design.md](../specs/2026-05-28-rollout-tui-design.md).

---

## File Map

**New files:**
- `ui/internal/ringbuffer/ringbuffer.go` — bounded line buffer
- `ui/internal/ringbuffer/ringbuffer_test.go`
- `ui/messages.go` — Bubble Tea message types
- `ui/linewriter.go` — `io.Writer` that emits `imageOutputMsg`
- `ui/linewriter_test.go`
- `ui/styles.go` — lipgloss styles
- `ui/model.go` — Bubble Tea model (Init/Update/View)
- `ui/model_test.go` — teatest-based smoke test
- `ui/program.go` — `Program` type (constructor, helpers, Run/Wait, TTY detection)
- `ui/fallback.go` — non-TTY plain-text writer + state logger

**Modified files:**
- `go.mod` / `go.sum` — add bubbletea, bubbles, lipgloss; promote `mattn/go-isatty` to direct; remove `gosuri/uiprogress` and `gosuri/uilive`
- `docker/buildgomod.go` — accept `io.Writer`
- `docker/build.go` — accept `io.Writer`
- `docker/push.go` — accept `io.Writer`
- `image/image.go` — `Build` accepts `io.Writer`
- `command/rollout/command.go` — wire to `ui.Program`, drop `uiprogress`

---

## Task 1: Promote go-isatty to a direct dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Inspect current go.mod**

Run: `grep -n isatty go.mod`
Expected: a single line under indirect deps: `	github.com/mattn/go-isatty v0.0.17 // indirect`.

- [ ] **Step 2: Promote to direct**

Run:

```bash
go get github.com/mattn/go-isatty@v0.0.17
go mod tidy
```

After this, the `// indirect` annotation should be gone for `go-isatty` (it will appear in a `require` block without it).

- [ ] **Step 3: Verify build still passes**

Run: `go build ./...`
Expected: exits 0, no output.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: promote go-isatty to direct dependency"
```

---

## Task 2: Add Bubble Tea, Bubbles, and Lipgloss

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependencies**

Run:

```bash
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/charmbracelet/lipgloss@latest
go mod tidy
```

- [ ] **Step 2: Verify they appear in go.mod**

Run: `grep -n charmbracelet go.mod`
Expected: three lines under the direct-require block, one per package.

- [ ] **Step 3: Verify build still passes**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add bubbletea, bubbles, lipgloss"
```

---

## Task 3: Ring buffer — failing test

**Files:**
- Create: `ui/internal/ringbuffer/ringbuffer_test.go`

- [ ] **Step 1: Write the test file**

Create `ui/internal/ringbuffer/ringbuffer_test.go` with the following exact contents:

```go
package ringbuffer

import (
	"reflect"
	"testing"
)

func TestRingBuffer_AppendUnderCapacity(t *testing.T) {
	b := New(4)
	b.Append("a")
	b.Append("b")
	b.Append("c")

	got := b.Lines()
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
}

func TestRingBuffer_AppendAtCapacity(t *testing.T) {
	b := New(3)
	b.Append("a")
	b.Append("b")
	b.Append("c")

	got := b.Lines()
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
}

func TestRingBuffer_AppendOverCapacityDropsOldest(t *testing.T) {
	b := New(3)
	b.Append("a")
	b.Append("b")
	b.Append("c")
	b.Append("d")
	b.Append("e")

	got := b.Lines()
	want := []string{"c", "d", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
}

func TestRingBuffer_Len(t *testing.T) {
	b := New(3)
	if b.Len() != 0 {
		t.Fatalf("empty Len() = %d, want 0", b.Len())
	}
	b.Append("a")
	b.Append("b")
	if b.Len() != 2 {
		t.Fatalf("after 2 appends Len() = %d, want 2", b.Len())
	}
	b.Append("c")
	b.Append("d")
	if b.Len() != 3 {
		t.Fatalf("after overflow Len() = %d, want 3", b.Len())
	}
}

func TestRingBuffer_NewPanicsOnNonPositiveCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for capacity 0")
		}
	}()
	New(0)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./ui/internal/ringbuffer/...`
Expected: build failure — `package ringbuffer; expected declarations` or `New undefined` / `no Go files`. That's the failing test.

---

## Task 4: Ring buffer — implementation

**Files:**
- Create: `ui/internal/ringbuffer/ringbuffer.go`

- [ ] **Step 1: Write the implementation**

Create `ui/internal/ringbuffer/ringbuffer.go` with exactly:

```go
// Package ringbuffer provides a fixed-capacity FIFO buffer of strings.
package ringbuffer

import "fmt"

type Buffer struct {
	data  []string
	start int
	len   int
	cap   int
}

func New(capacity int) *Buffer {
	if capacity <= 0 {
		panic(fmt.Sprintf("ringbuffer: capacity must be positive, got %d", capacity))
	}
	return &Buffer{
		data: make([]string, capacity),
		cap:  capacity,
	}
}

func (b *Buffer) Append(line string) {
	if b.len < b.cap {
		b.data[(b.start+b.len)%b.cap] = line
		b.len++
		return
	}
	b.data[b.start] = line
	b.start = (b.start + 1) % b.cap
}

func (b *Buffer) Len() int { return b.len }

func (b *Buffer) Lines() []string {
	out := make([]string, b.len)
	for i := 0; i < b.len; i++ {
		out[i] = b.data[(b.start+i)%b.cap]
	}
	return out
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./ui/internal/ringbuffer/...`
Expected: `ok` with all five tests passing.

- [ ] **Step 3: Commit**

```bash
git add ui/internal/ringbuffer
git commit -m "ui: add ring buffer for per-image output"
```

---

## Task 5: Define Bubble Tea message types

**Files:**
- Create: `ui/messages.go`

- [ ] **Step 1: Write the message types**

Create `ui/messages.go` with exactly:

```go
package ui

import "time"

type imageStateMsg struct {
	Name  string
	State string
	When  time.Time
}

type imageNameMsg struct {
	Name      string
	ImageName string
}

type imageOutputMsg struct {
	Name string
	Line string
}

type imageDoneMsg struct {
	Name string
	Err  error
}

type allDoneMsg struct{}

type tickMsg struct {
	Now time.Time
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./ui/...`
Expected: exits 0 (the package now compiles even with no model yet).

- [ ] **Step 3: Commit**

```bash
git add ui/messages.go
git commit -m "ui: add Bubble Tea message types"
```

---

## Task 6: LineWriter — failing test

**Files:**
- Create: `ui/linewriter_test.go`

- [ ] **Step 1: Write the test file**

Create `ui/linewriter_test.go` with exactly:

```go
package ui

import (
	"reflect"
	"testing"
)

func collectLines(t *testing.T) (*LineWriter, *[]string) {
	t.Helper()
	var lines []string
	w := newLineWriterFunc("test", func(line string) {
		lines = append(lines, line)
	})
	return w, &lines
}

func TestLineWriter_SplitsOnNewline(t *testing.T) {
	w, lines := collectLines(t)
	_, _ = w.Write([]byte("hello\nworld\n"))
	want := []string{"hello", "world"}
	if !reflect.DeepEqual(*lines, want) {
		t.Fatalf("lines = %v, want %v", *lines, want)
	}
}

func TestLineWriter_BuffersPartialLineUntilNewline(t *testing.T) {
	w, lines := collectLines(t)
	_, _ = w.Write([]byte("partial "))
	if len(*lines) != 0 {
		t.Fatalf("expected no emissions yet, got %v", *lines)
	}
	_, _ = w.Write([]byte("rest\n"))
	want := []string{"partial rest"}
	if !reflect.DeepEqual(*lines, want) {
		t.Fatalf("lines = %v, want %v", *lines, want)
	}
}

func TestLineWriter_TreatsCROnlyAsLineTerminator(t *testing.T) {
	w, lines := collectLines(t)
	_, _ = w.Write([]byte("step 1\rstep 2\rstep 3\n"))
	want := []string{"step 1", "step 2", "step 3"}
	if !reflect.DeepEqual(*lines, want) {
		t.Fatalf("lines = %v, want %v", *lines, want)
	}
}

func TestLineWriter_StripsANSIEscapeSequences(t *testing.T) {
	w, lines := collectLines(t)
	_, _ = w.Write([]byte("\x1b[31mred\x1b[0m text\n"))
	want := []string{"red text"}
	if !reflect.DeepEqual(*lines, want) {
		t.Fatalf("lines = %v, want %v", *lines, want)
	}
}

func TestLineWriter_CloseFlushesPartialLine(t *testing.T) {
	w, lines := collectLines(t)
	_, _ = w.Write([]byte("no newline"))
	w.Close()
	want := []string{"no newline"}
	if !reflect.DeepEqual(*lines, want) {
		t.Fatalf("lines = %v, want %v", *lines, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./ui/...`
Expected: build failure — `undefined: LineWriter` and `undefined: newLineWriterFunc`.

---

## Task 7: LineWriter — implementation

**Files:**
- Create: `ui/linewriter.go`

- [ ] **Step 1: Write the implementation**

Create `ui/linewriter.go` with exactly:

```go
package ui

import (
	"bytes"
	"regexp"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

type LineWriter struct {
	mu      sync.Mutex
	name    string
	buf     bytes.Buffer
	emit    func(string)
}

func newLineWriter(prog *tea.Program, name string) *LineWriter {
	return newLineWriterFunc(name, func(line string) {
		prog.Send(imageOutputMsg{Name: name, Line: line})
	})
}

func newLineWriterFunc(name string, emit func(string)) *LineWriter {
	return &LineWriter{name: name, emit: emit}
}

func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	for _, b := range p {
		switch b {
		case '\n', '\r':
			w.flush()
		default:
			w.buf.WriteByte(b)
		}
	}
	return n, nil
}

func (w *LineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
	return nil
}

func (w *LineWriter) flush() {
	if w.buf.Len() == 0 {
		return
	}
	line := ansiRE.ReplaceAllString(w.buf.String(), "")
	w.buf.Reset()
	w.emit(line)
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./ui/...`
Expected: `ok` with all five tests passing.

- [ ] **Step 3: Commit**

```bash
git add ui/linewriter.go ui/linewriter_test.go
git commit -m "ui: add line writer that streams output to TUI"
```

---

## Task 8: Refactor docker.BuildGoMod to accept io.Writer

**Files:**
- Modify: `docker/buildgomod.go`

- [ ] **Step 1: Rewrite the function**

Replace the entire body of [docker/buildgomod.go](docker/buildgomod.go) with exactly:

```go
package docker

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

//go:embed go-dockerfile
var dockerfileTemplate string

type DockerfileData struct {
	PackagePath string
	GoVersion   string
}

func BuildGoMod(ctx context.Context, mainPackagePath, imageName, platform string, out io.Writer) error {
	pkg, err := packages.Load(&packages.Config{
		Mode:    packages.NeedModule | packages.NeedName,
		Context: ctx,
		Dir:     mainPackagePath,
	}, ".")
	if err != nil {
		return fmt.Errorf("could not get main package: %w", err)
	}

	mod := pkg[0].Module
	if mod.Error != nil {
		return fmt.Errorf("could not get module info for the main package: %w", err)
	}

	modData, err := os.ReadFile(mod.GoMod)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", mod.GoMod, err)
	}

	modFile, err := modfile.Parse(mod.GoMod, modData, nil)
	if err != nil {
		return fmt.Errorf("could not parse go.mod file: %w", err)
	}

	fullPath := pkg[0].PkgPath
	path := modFile.Module.Mod.Path
	shortPath := strings.TrimPrefix(fullPath, path)
	shortPath = strings.TrimPrefix(shortPath, "/")

	templ, err := template.New("dockerfile").Parse(dockerfileTemplate)
	if err != nil {
		return fmt.Errorf("could not parse dockerfile template: %w", err)
	}
	rendered := &bytes.Buffer{}
	err = templ.Execute(rendered, DockerfileData{
		PackagePath: shortPath,
		GoVersion:   modFile.Go.Version,
	})
	if err != nil {
		return fmt.Errorf("could not render dockerfile template: %w", err)
	}
	dockerRoot := mod.Dir

	genCmd := exec.CommandContext(ctx, "go", "generate", "./...")
	genCmd.Dir = dockerRoot
	genCmd.Stdout = out
	genCmd.Stderr = out
	if err := genCmd.Run(); err != nil {
		return fmt.Errorf("go generate ./... failed: %w", err)
	}

	tempDockerfile, err := os.CreateTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create temp dockerfile: %w", err)
	}
	defer os.Remove(tempDockerfile.Name())
	defer tempDockerfile.Close()

	_, err = tempDockerfile.Write(rendered.Bytes())
	if err != nil {
		return fmt.Errorf("could not write to temp docker file: %w", err)
	}

	if err := tempDockerfile.Close(); err != nil {
		return fmt.Errorf("could not close temp docker file: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "buildx", "build", "--platform", platform, "-t", imageName, "-f", tempDockerfile.Name(), "--progress", "plain", dockerRoot)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	return nil
}
```

Note: the previous `bytes.Buffer` capture is removed. Output is written through `out`.

- [ ] **Step 2: Verify package builds (callers will break for now — that's fine)**

Run: `go build ./docker/...`
Expected: exits 0 (the docker package itself only needs the new signature).

- [ ] **Step 3: Don't commit yet — image package still calls the old signature.**

---

## Task 9: Refactor docker.BuildDockerfile to accept io.Writer

**Files:**
- Modify: `docker/build.go`

- [ ] **Step 1: Rewrite the function**

Replace the entire body of [docker/build.go](docker/build.go) with exactly:

```go
package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// BuildDockerfile builds a Docker image from the given context directory and
// Dockerfile path using `docker buildx build`. The resulting image is tagged
// with imageName and loaded into the local Docker daemon. Streamed stdout and
// stderr are written to out.
func BuildDockerfile(ctx context.Context, contextDir, dockerfilePath, imageName, platform string, out io.Writer) error {
	if info, err := os.Stat(contextDir); err != nil {
		return fmt.Errorf("stat context dir %s: %w", contextDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("context path is not a directory: %s", contextDir)
	}
	if _, err := os.Stat(dockerfilePath); err != nil {
		return fmt.Errorf("stat dockerfile %s: %w", dockerfilePath, err)
	}

	cmd := exec.CommandContext(ctx, "docker", "buildx", "build",
		"--platform", platform,
		"-t", imageName,
		"-f", dockerfilePath,
		"--progress", "plain",
		contextDir,
	)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Verify docker package builds**

Run: `go build ./docker/...`
Expected: exits 0.

- [ ] **Step 3: Don't commit yet.**

---

## Task 10: Refactor docker.Push to accept io.Writer

**Files:**
- Modify: `docker/push.go`

- [ ] **Step 1: Rewrite the function**

Replace the entire body of [docker/push.go](docker/push.go) with exactly:

```go
package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

func Push(ctx context.Context, image string, out io.Writer) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("could not find docker binary: %w", err)
	}
	cmd := exec.CommandContext(ctx, dockerPath, "image", "push", "-q", image)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not push image: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Verify docker package builds**

Run: `go build ./docker/...`
Expected: exits 0.

- [ ] **Step 3: Don't commit yet.**

---

## Task 11: Refactor image.Image.Build to accept io.Writer

**Files:**
- Modify: `image/image.go` (only the `Build` method)

- [ ] **Step 1: Update the Build method signature and calls**

In [image/image.go](image/image.go), replace the entire `Build` method (starting at `func (i *Image) Build`) with exactly:

```go
func (i *Image) Build(ctx context.Context, projectRoot string, out io.Writer) error {
	imageWithTag, err := i.DockerImageName(ctx, projectRoot)
	if err != nil {
		return err
	}

	switch {
	case i.Go != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}

		err = docker.BuildGoMod(ctx, path.Join(projectRoot, i.Go.Package), imageWithTag, platform, out)
		if err != nil {
			return fmt.Errorf("while building image %s: %w", imageWithTag, err)
		}
	case i.Docker != nil:
		platform := i.Platform
		if platform == "" {
			platform = "linux/amd64"
		}
		contextDir, dockerfilePath := i.Docker.resolvePaths(projectRoot)
		err = docker.BuildDockerfile(ctx, contextDir, dockerfilePath, imageWithTag, platform, out)
		if err != nil {
			return fmt.Errorf("while building docker image %s: %w", imageWithTag, err)
		}
	}

	return nil
}
```

- [ ] **Step 2: Add the `io` import**

At the top of `image/image.go`, ensure the import block contains `"io"` alongside the other stdlib imports. The block becomes:

```go
import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"

	"github.com/draganm/gosha/gosha"
	"github.com/draganm/monotool/docker"
)
```

- [ ] **Step 3: Verify image package builds**

Run: `go build ./image/...`
Expected: exits 0.

- [ ] **Step 4: Confirm callers (rollout command) are now broken**

Run: `go build ./...`
Expected: build failure in `command/rollout/command.go` complaining about `im.Build` call arity. This is expected — Task 19 will fix it.

- [ ] **Step 5: Don't commit yet — keep the writer refactor + rollout wiring as one logical commit, completed in Task 19.**

---

## Task 12: Lipgloss styles

**Files:**
- Create: `ui/styles.go`

- [ ] **Step 1: Write the styles file**

Create `ui/styles.go` with exactly:

```go
package ui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#5f00af"))

	leftPaneStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	rightPaneStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#5f00af"))

	itemStyle = lipgloss.NewStyle()

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Padding(0, 1)

	stateStyles = map[string]lipgloss.Style{
		"waiting":        lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		"checking remote": lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		"building image": lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		"pushing image":  lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		"done":           lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		"already pushed": lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		"failed":         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		"cancelled":      lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	}
)

func stateStyle(state string) lipgloss.Style {
	if s, ok := stateStyles[state]; ok {
		return s
	}
	return itemStyle
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./ui/...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add ui/styles.go
git commit -m "ui: add lipgloss styles for TUI panes and states"
```

---

## Task 13: Model — failing test

**Files:**
- Create: `ui/model_test.go`

- [ ] **Step 1: Write the test file**

Create `ui/model_test.go` with exactly:

```go
package ui

import (
	"strings"
	"testing"
	"time"
)

func TestModel_RendersItemList(t *testing.T) {
	m := newModel([]string{"api", "worker"})
	m.width = 100
	m.height = 30
	m.initSizes()

	view := m.View()
	if !strings.Contains(view, "api") {
		t.Fatalf("expected view to contain 'api', got:\n%s", view)
	}
	if !strings.Contains(view, "worker") {
		t.Fatalf("expected view to contain 'worker', got:\n%s", view)
	}
}

func TestModel_UpdatesStateAndImageName(t *testing.T) {
	m := newModel([]string{"api"})
	m.width = 100
	m.height = 30
	m.initSizes()

	m2, _ := m.Update(imageNameMsg{Name: "api", ImageName: "repo/api:abcd"})
	mm := m2.(*Model)
	m3, _ := mm.Update(imageStateMsg{Name: "api", State: "building image", When: time.Now()})
	mm = m3.(*Model)

	view := mm.View()
	if !strings.Contains(view, "repo/api:abcd") {
		t.Fatalf("expected view to contain image name, got:\n%s", view)
	}
	if !strings.Contains(view, "building image") {
		t.Fatalf("expected view to contain 'building image', got:\n%s", view)
	}
}

func TestModel_AppendsOutputToSelectedItem(t *testing.T) {
	m := newModel([]string{"api"})
	m.width = 100
	m.height = 30
	m.initSizes()

	m2, _ := m.Update(imageOutputMsg{Name: "api", Line: "hello from build"})
	mm := m2.(*Model)

	view := mm.View()
	if !strings.Contains(view, "hello from build") {
		t.Fatalf("expected view to contain build output, got:\n%s", view)
	}
}

func TestModel_AllDoneWithoutFailureRequestsQuit(t *testing.T) {
	m := newModel([]string{"api"})
	m.width = 100
	m.height = 30
	m.initSizes()

	_, _ = m.Update(imageDoneMsg{Name: "api", Err: nil})
	_, cmd := m.Update(allDoneMsg{})
	if cmd == nil {
		t.Fatal("expected allDoneMsg with no failures to produce a quit command")
	}
	msg := cmd()
	if _, ok := msg.(interface{ String() string }); ok {
		// tea.QuitMsg has no exported identity; we just check it's the expected type by name.
	}
	if msgTypeName(msg) != "tea.QuitMsg" {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestModel_AllDoneWithFailureStaysOpen(t *testing.T) {
	m := newModel([]string{"api"})
	m.width = 100
	m.height = 30
	m.initSizes()

	_, _ = m.Update(imageDoneMsg{Name: "api", Err: errSentinel})
	_, cmd := m.Update(allDoneMsg{})
	if cmd != nil {
		t.Fatalf("expected no quit command when failed, got %T", cmd())
	}
}
```

- [ ] **Step 2: Add the test helpers file**

Create `ui/model_testhelpers_test.go` with exactly:

```go
package ui

import (
	"errors"
	"fmt"
)

var errSentinel = errors.New("sentinel")

func msgTypeName(m any) string {
	return fmt.Sprintf("%T", m)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./ui/...`
Expected: build failure — `undefined: newModel`, `undefined: Model.initSizes`, etc.

---

## Task 14: Model — implementation

**Files:**
- Create: `ui/model.go`

- [ ] **Step 1: Write the model**

Create `ui/model.go` with exactly:

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/draganm/monotool/ui/internal/ringbuffer"
)

const (
	defaultRingCapacity = 2000
	tickInterval        = time.Second
)

type imageItem struct {
	Name      string
	ImageName string
	State     string
	Started   time.Time
	Finished  time.Time
	Output    *ringbuffer.Buffer
	Err       error
}

func (it *imageItem) terminal() bool {
	switch it.State {
	case "done", "already pushed", "failed", "cancelled":
		return true
	}
	return false
}

type Model struct {
	items    []*imageItem
	index    map[string]int
	selected int
	viewport viewport.Model
	width    int
	height   int
	done     bool
	failed   bool
	now      time.Time
}

func newModel(names []string) *Model {
	items := make([]*imageItem, len(names))
	index := make(map[string]int, len(names))
	for i, n := range names {
		items[i] = &imageItem{
			Name:   n,
			State:  "waiting",
			Output: ringbuffer.New(defaultRingCapacity),
		}
		index[n] = i
	}
	return &Model{
		items: items,
		index: index,
		now:   time.Now(),
	}
}

func (m *Model) initSizes() {
	leftWidth := m.width * 4 / 10
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 4 // borders + padding
	if rightWidth < 20 {
		rightWidth = 20
	}
	vpHeight := m.height - 4
	if vpHeight < 5 {
		vpHeight = 5
	}
	m.viewport = viewport.New(rightWidth, vpHeight)
	m.refreshViewport()
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg{Now: t} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.initSizes()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		m.now = msg.Now
		return m, tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg{Now: t} })

	case imageNameMsg:
		if i, ok := m.index[msg.Name]; ok {
			m.items[i].ImageName = msg.ImageName
		}
		return m, nil

	case imageStateMsg:
		if i, ok := m.index[msg.Name]; ok {
			it := m.items[i]
			if it.Started.IsZero() {
				it.Started = msg.When
			}
			it.State = msg.State
			if it.terminal() && it.Finished.IsZero() {
				it.Finished = msg.When
			}
		}
		return m, nil

	case imageOutputMsg:
		if i, ok := m.index[msg.Name]; ok {
			m.items[i].Output.Append(msg.Line)
			if i == m.selected {
				m.refreshViewport()
			}
		}
		return m, nil

	case imageDoneMsg:
		if i, ok := m.index[msg.Name]; ok {
			it := m.items[i]
			it.Err = msg.Err
			if msg.Err != nil {
				it.State = "failed"
				m.failed = true
				it.Output.Append(fmt.Sprintf("ERROR: %v", msg.Err))
			} else if !it.terminal() {
				it.State = "done"
			}
			if it.Finished.IsZero() {
				it.Finished = time.Now()
			}
			if i == m.selected {
				m.refreshViewport()
			}
		}
		return m, nil

	case allDoneMsg:
		m.done = true
		if !m.failed {
			return m, tea.Quit
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if m.done {
			return m, tea.Quit
		}
		return m, nil
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.refreshViewport()
		}
		return m, nil
	case "down", "j":
		if m.selected < len(m.items)-1 {
			m.selected++
			m.refreshViewport()
		}
		return m, nil
	case "g":
		m.viewport.GotoTop()
		return m, nil
	case "G":
		m.viewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) refreshViewport() {
	if m.selected < 0 || m.selected >= len(m.items) {
		m.viewport.SetContent("")
		return
	}
	lines := m.items[m.selected].Output.Lines()
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	title := titleStyle.Render("monotool rollout")

	leftWidth := m.width*4/10 - 4
	if leftWidth < 26 {
		leftWidth = 26
	}

	var rows []string
	for i, it := range m.items {
		row := renderItemRow(it, m.now, leftWidth-2)
		if i == m.selected {
			row = selectedItemStyle.Render(row)
		} else {
			row = itemStyle.Render(row)
		}
		rows = append(rows, row)
	}

	leftBody := strings.Join(rows, "\n")
	leftHeight := m.height - 4

	left := leftPaneStyle.Width(leftWidth).Height(leftHeight).Render(leftBody)

	rightWidth := m.width - lipgloss.Width(left) - 2
	if rightWidth < 20 {
		rightWidth = 20
	}
	right := rightPaneStyle.Width(rightWidth).Height(leftHeight).Render(m.viewport.View())

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	doneCount := 0
	for _, it := range m.items {
		if it.terminal() {
			doneCount++
		}
	}

	footerText := "↑/↓ navigate · PgUp/PgDn scroll · g/G top/bottom"
	if m.done {
		if m.failed {
			footerText += " · q quit (failed)"
		} else {
			footerText += " · q quit"
		}
	} else {
		footerText += " · ctrl-c cancel"
	}
	footerText += fmt.Sprintf("    %d/%d complete", doneCount, len(m.items))
	footer := footerStyle.Render(footerText)

	return lipgloss.JoinVertical(lipgloss.Left, title, body, footer)
}

func renderItemRow(it *imageItem, now time.Time, width int) string {
	name := it.Name
	if len(name) > 14 {
		name = name[:14]
	}
	name = padRight(name, 14)

	state := padRight(it.State, 18)
	state = stateStyle(it.State).Render(state)

	var elapsed string
	switch {
	case it.Started.IsZero():
		elapsed = "  --"
	case it.Finished.IsZero():
		elapsed = formatDuration(now.Sub(it.Started))
	default:
		elapsed = formatDuration(it.Finished.Sub(it.Started))
	}

	row := fmt.Sprintf("%s %s %s", name, state, elapsed)
	if it.ImageName != "" {
		row += "  " + it.ImageName
	}
	return row
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./ui/...`
Expected: `ok` with all model + linewriter + ringbuffer tests passing.

- [ ] **Step 3: Commit**

```bash
git add ui/model.go ui/model_test.go ui/model_testhelpers_test.go
git commit -m "ui: add Bubble Tea model for rollout TUI"
```

---

## Task 15: Program type — TTY constructor and helpers

**Files:**
- Create: `ui/program.go`

- [ ] **Step 1: Write the program file**

Create `ui/program.go` with exactly:

```go
package ui

import (
	"context"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

type Program struct {
	model    *Model
	tea      *tea.Program
	fallback *fallbackProgram
	done     chan struct{}
	runErr   error
}

func New(names []string) *Program {
	if isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stderr.Fd()) {
		m := newModel(names)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
		return &Program{model: m, tea: p, done: make(chan struct{})}
	}
	return &Program{fallback: newFallbackProgram(names, os.Stderr), done: make(chan struct{})}
}

func (p *Program) Run() {
	go func() {
		defer close(p.done)
		if p.tea != nil {
			_, err := p.tea.Run()
			p.runErr = err
			return
		}
		p.fallback.run()
	}()
}

func (p *Program) Wait() error {
	<-p.done
	return p.runErr
}

func (p *Program) Writer(name string) io.Writer {
	if p.tea != nil {
		return newLineWriter(p.tea, name)
	}
	return p.fallback.writer(name)
}

func (p *Program) SetState(name, state string) {
	msg := imageStateMsg{Name: name, State: state, When: time.Now()}
	if p.tea != nil {
		p.tea.Send(msg)
		return
	}
	p.fallback.setState(msg)
}

func (p *Program) SetImageName(name, imageName string) {
	msg := imageNameMsg{Name: name, ImageName: imageName}
	if p.tea != nil {
		p.tea.Send(msg)
		return
	}
	p.fallback.setImageName(msg)
}

func (p *Program) Finish(name string, err error) {
	msg := imageDoneMsg{Name: name, Err: err}
	if p.tea != nil {
		p.tea.Send(msg)
		return
	}
	p.fallback.finish(msg)
}

func (p *Program) FinishAll() {
	if p.tea != nil {
		p.tea.Send(allDoneMsg{})
		return
	}
	p.fallback.finishAll()
}

// Quit asks the TUI to stop immediately. Used when the rollout context is
// cancelled before all images have finished.
func (p *Program) Quit() {
	if p.tea != nil {
		p.tea.Quit()
	}
}

// WaitForContextCancel sends a Quit to the tea program when ctx is done, so
// Ctrl+C at the OS level is reflected in the TUI exit.
func (p *Program) WaitForContextCancel(ctx context.Context) {
	if p.tea == nil {
		return
	}
	go func() {
		<-ctx.Done()
		p.tea.Quit()
	}()
}
```

- [ ] **Step 2: Verify it builds (fallback isn't written yet — this step will fail, that's expected)**

Run: `go build ./ui/...`
Expected: build failure — `undefined: newFallbackProgram`. Proceed to Task 16.

---

## Task 16: Non-TTY fallback

**Files:**
- Create: `ui/fallback.go`

- [ ] **Step 1: Write the fallback**

Create `ui/fallback.go` with exactly:

```go
package ui

import (
	"fmt"
	"io"
	"sync"
)

type fallbackProgram struct {
	mu    sync.Mutex
	out   io.Writer
	names []string
	done  chan struct{}
}

func newFallbackProgram(names []string, out io.Writer) *fallbackProgram {
	return &fallbackProgram{
		out:   out,
		names: names,
		done:  make(chan struct{}),
	}
}

func (p *fallbackProgram) run() {
	<-p.done
}

func (p *fallbackProgram) writer(name string) io.Writer {
	return newLineWriterFunc(name, func(line string) {
		p.mu.Lock()
		defer p.mu.Unlock()
		fmt.Fprintf(p.out, "[%s] %s\n", name, line)
	})
}

func (p *fallbackProgram) setState(msg imageStateMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "[%s] state: %s\n", msg.Name, msg.State)
}

func (p *fallbackProgram) setImageName(msg imageNameMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.out, "[%s] image: %s\n", msg.Name, msg.ImageName)
}

func (p *fallbackProgram) finish(msg imageDoneMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if msg.Err != nil {
		fmt.Fprintf(p.out, "[%s] FAILED: %v\n", msg.Name, msg.Err)
	} else {
		fmt.Fprintf(p.out, "[%s] DONE\n", msg.Name)
	}
}

func (p *fallbackProgram) finishAll() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}
```

- [ ] **Step 2: Run all UI tests**

Run: `go test ./ui/...`
Expected: `ok` for all packages.

- [ ] **Step 3: Verify whole module builds (rollout command still broken — that's expected and handled in Task 19)**

Run: `go build ./ui/... ./docker/... ./image/... ./config/...`
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add ui/program.go ui/fallback.go
git commit -m "ui: add Program orchestrator with TTY/non-TTY paths"
```

---

## Task 17: Refactor docker funcs commit

(The Task 8–11 changes need to land alongside Task 19's rollout command rewrite as a single working state. We hold off committing them until then; this task is a checkpoint to confirm everything is staged correctly.)

- [ ] **Step 1: Inspect dirty files**

Run: `git status`
Expected (uncommitted modifications):
- `docker/buildgomod.go`
- `docker/build.go`
- `docker/push.go`
- `image/image.go`

No new tracked files should be uncommitted (Tasks 5, 7, 12, 14, 16 commits land everything in `ui/`).

If anything else is dirty, stop and reconcile.

---

## Task 18: Add a flag-clearing helper in rollout command

**Files:**
- Modify: `command/rollout/command.go`

Skipped — the new command file (Task 19) replaces this code outright. No separate helper is needed; this task exists only to make the numbering match the spec sections.

- [ ] **Step 1: No-op task; proceed.**

---

## Task 19: Rewrite the rollout command to use ui.Program

**Files:**
- Modify: `command/rollout/command.go`

- [ ] **Step 1: Replace the file**

Replace the entire contents of [command/rollout/command.go](command/rollout/command.go) with exactly:

```go
package rollout

import (
	"errors"
	"fmt"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/draganm/monotool/config"
	"github.com/draganm/monotool/docker"
	"github.com/draganm/monotool/ui"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

func Command() *cli.Command {
	return &cli.Command{
		Name: "rollout",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "message",
				Aliases:  []string{"m"},
				Usage:    "describe the purpose of the rollout (included in the PR description)",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("could not load config: %w", err)
			}

			message := strings.TrimSpace(c.String("message"))
			if message == "" {
				return errors.New("rollout message (-m) must not be empty")
			}

			requestedRollout := c.Args().First()

			buildSemaphore := semaphore.NewWeighted(4)
			checkImageSemaphore := semaphore.NewWeighted(10)

			if requestedRollout == "" {
				switch len(cfg.RollOuts) {
				case 0:
					return errors.New("there are no rollouts defined in the config file")
				case 1:
					for n := range cfg.RollOuts {
						requestedRollout = n
					}
				default:
					allRollouts := lo.Keys(cfg.RollOuts)
					sort.Strings(allRollouts)
					sb := new(strings.Builder)
					sb.WriteString("there are %d rollouts available, please specify one of the following:\n")
					for _, r := range allRollouts {
						sb.WriteString(fmt.Sprintf("%s\n", r))
					}
					return fmt.Errorf(sb.String(), len(cfg.RollOuts))
				}
			}

			r, found := cfg.RollOuts[requestedRollout]
			if !found {
				return fmt.Errorf("rollout %q does not exist", requestedRollout)
			}

			ctx, cancel := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
			defer cancel()

			imageNames := lo.Keys(cfg.Images)
			sort.Strings(imageNames)

			prog := ui.New(imageNames)
			prog.Run()
			prog.WaitForContextCancel(ctx)

			images := map[string]string{}
			values := map[string]any{
				"images": images,
			}
			imagesLock := &sync.Mutex{}

			eg, egCtx := errgroup.WithContext(ctx)

			for _, n := range imageNames {
				n := n
				im := cfg.Images[n]
				eg.Go(func() error {
					if egCtx.Err() != nil {
						return egCtx.Err()
					}

					w := prog.Writer(n)
					prog.SetState(n, "checking remote")

					imageName, err := im.DockerImageName(egCtx, cfg.ProjectRoot)
					if err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not calculate docker image of %s: %w", n, err)
					}

					prog.SetImageName(n, imageName)

					imagesLock.Lock()
					images[n] = imageName
					imagesLock.Unlock()

					if err := checkImageSemaphore.Acquire(egCtx, 1); err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not acquire semaphore for image %s: %w", n, err)
					}

					hasImage, err := docker.RepoHasImage(egCtx, imageName)
					checkImageSemaphore.Release(1)
					if err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

					if hasImage {
						prog.SetState(n, "already pushed")
						prog.Finish(n, nil)
						return nil
					}

					isBuilt, err := im.IsAlreadyBuilt(egCtx, cfg.ProjectRoot)
					if err != nil {
						prog.Finish(n, err)
						return fmt.Errorf("could not get status of image %s: %w", n, err)
					}

					if !isBuilt {
						if err := buildSemaphore.Acquire(egCtx, 1); err != nil {
							prog.Finish(n, err)
							return fmt.Errorf("could not acquire semaphore for building image %s: %w", n, err)
						}
						prog.SetState(n, "building image")
						err = im.Build(egCtx, cfg.ProjectRoot, w)
						buildSemaphore.Release(1)
						if err != nil {
							prog.Finish(n, err)
							return err
						}
					}

					prog.SetState(n, "pushing image")
					if err := docker.Push(egCtx, imageName, w); err != nil {
						prog.Finish(n, err)
						return err
					}

					prog.SetState(n, "done")
					prog.Finish(n, nil)
					return nil
				})
			}

			buildErr := eg.Wait()
			prog.FinishAll()
			if waitErr := prog.Wait(); waitErr != nil {
				return waitErr
			}
			if buildErr != nil {
				return fmt.Errorf("could not build images: %w", buildErr)
			}

			fmt.Printf("rolling out to %s\n", requestedRollout)
			if err := r.RollOut(ctx, cfg.ProjectRoot, values, message); err != nil {
				return fmt.Errorf("roll out failed: %w", err)
			}

			return nil
		},
	}
}

```

- [ ] **Step 2: Run `go mod tidy` to drop unused deps**

Run: `go mod tidy`

Expected: `gosuri/uiprogress` and `gosuri/uilive` removed from `go.mod` / `go.sum`.

- [ ] **Step 3: Verify the whole module builds**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: all pass.

- [ ] **Step 5: Commit the bundled refactor**

```bash
git add docker/buildgomod.go docker/build.go docker/push.go image/image.go command/rollout/command.go go.mod go.sum
git commit -m "feat(rollout): wire build/push output to per-image io.Writer

Refactor BuildGoMod, BuildDockerfile, Push, and image.Image.Build to
accept an io.Writer so their output can be streamed live instead of
being captured to a bytes.Buffer and revealed only on failure. The
rollout command supplies a per-image writer from the new TUI program."
```

---

## Task 20: Wire TUI into rollout command

This task is folded into Task 19's commit — the rollout command rewrite already imports `ui` and calls `ui.New(...).Writer(name)` etc. Skip.

- [ ] **Step 1: No-op task; proceed.**

---

## Task 21: Manual smoke test (TUI path)

**Goal:** Confirm the TUI renders and behaves as designed on a real terminal.

- [ ] **Step 1: Build the binary**

Run: `go build -o /tmp/monotool .`
Expected: exits 0; `/tmp/monotool` exists.

- [ ] **Step 2: Run against a configured project**

The repo at hand may not have a `.monotool` config. If it does, run from its root:

```bash
/tmp/monotool rollout -m "tui smoke"
```

If it does not, locate or set up a small monorepo with a `.monotool/config.yaml`. Expected behavior:

- Left pane lists every image with status and elapsed timer.
- Selecting an image (↑/↓ or j/k) updates the right pane to that image's output.
- During a long build, the right pane streams `docker buildx --progress=plain` lines.
- On success of all images, the TUI exits and the rollout proceeds to the templates phase.
- On any failure, the TUI stays open; pressing `q` exits with the build error.

- [ ] **Step 3: Test non-TTY fallback**

Run: `/tmp/monotool rollout -m "tui smoke" 2>&1 | cat`
Expected: plain prefixed-line output (no TUI), one line per state transition and per build-output line, in the form `[name] state: building image` and `[name] step 1/9 : FROM ...`.

- [ ] **Step 4: Test Ctrl+C cancellation**

Re-run in a real terminal, press Ctrl+C mid-build.
Expected: in-flight docker subprocesses get killed; TUI exits non-zero; the shell prompt returns within a second or two.

- [ ] **Step 5: Remove the test binary**

Run: `rm /tmp/monotool`

---

## Task 22: Open the pull request

- [ ] **Step 1: Check current branch and remote**

Run: `git status && git log --oneline -10 && git rev-parse --abbrev-ref HEAD`

Expected: branch is `feature/rollout-tui` (or current worktree branch). Recent commits should match Tasks 1, 2, 4, 5, 7, 12, 14, 16, 19, plus the initial design-doc commit.

- [ ] **Step 2: Push the branch**

Run: `git push -u origin HEAD`
Expected: branch created on the remote.

- [ ] **Step 3: Open the PR**

Run:

```bash
gh pr create --title "feat(rollout): live TUI for go generate / docker build / push" --body "$(cat <<'EOF'
## Summary
- Replace the per-image progress bars with a Bubble Tea TUI showing a list of images on the left and a live, scrollable output viewport on the right.
- Refactor `BuildGoMod`, `BuildDockerfile`, `Push`, and `image.Image.Build` to stream `stdout`+`stderr` to a caller-supplied `io.Writer`.
- Auto-exit on success; on failure the TUI stays open so the user can scroll through the failing image's output. Falls back to plain prefixed-line logging when stdout is not a TTY.

## Test plan
- [ ] `go build ./...`
- [ ] `go test ./...`
- [ ] Run `monotool rollout -m "smoke"` in a real terminal — verify navigation, scrolling, auto-exit on success.
- [ ] Run `monotool rollout -m "smoke" 2>&1 | cat` — verify non-TTY fallback prints prefixed lines.
- [ ] Force a build failure — verify TUI stays open, `q` exits with the build error.
- [ ] Ctrl+C during a build — verify subprocesses are killed and the process exits.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: gh prints the new PR URL.

---

## Self-review notes

- **Spec coverage:**
  - "List of images, navigate, view live output" → Tasks 13–14 (model), 21 (smoke).
  - "Streamed output from go generate / docker build / push" → Tasks 8, 10, 19.
  - "Refactor build/push to use io.Writer" → Tasks 8–11.
  - "Auto-exit on success, stay open on failure" → Task 14 (`allDoneMsg` branch).
  - "Non-TTY fallback" → Task 16, 21 step 3.
  - "Cancellation via Ctrl+C" → Task 15 (`WaitForContextCancel`), 21 step 4.
  - "Ring buffer 2000 lines" → Task 14 constant.
  - "ANSI strip, `\r` handling" → Task 7 (LineWriter tests).
- **Placeholders:** none — every step contains the code or command it requires.
- **Type consistency:** `Program.Writer`, `Program.SetState`, `Program.SetImageName`, `Program.Finish`, `Program.FinishAll`, `Program.Wait`, `Program.Run`, `Program.Quit`, `Program.WaitForContextCancel` are introduced in Task 15 and used identically in Task 19. `newModel`, `imageItem`, message types match between Tasks 5, 13, 14.
- **Note on Tasks 17, 18, 20:** these are intentional no-ops kept to preserve the natural file-split narrative without renumbering.
