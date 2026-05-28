package ui

import (
	"bytes"
	"regexp"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

type LineWriter struct {
	mu   sync.Mutex
	name string
	buf  bytes.Buffer
	emit func(string)
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
