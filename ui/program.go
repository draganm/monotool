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

func (p *Program) Quit() {
	if p.tea != nil {
		p.tea.Quit()
	}
}

func (p *Program) WaitForContextCancel(ctx context.Context) {
	if p.tea == nil {
		return
	}
	go func() {
		<-ctx.Done()
		p.tea.Quit()
	}()
}
