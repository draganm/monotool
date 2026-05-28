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
