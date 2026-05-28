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
