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

func TestWarnOmitsAbortFraming(t *testing.T) {
	s := New()
	s.Add("apps/foo/a.yaml", ReasonUnmarked)
	s.Add("apps/foo/b.yaml", ReasonHashMismatch)

	buf := new(bytes.Buffer)
	s.Warn(buf)
	out := buf.String()

	if strings.Contains(out, "rollout aborted") {
		t.Errorf("warn should not mention 'rollout aborted'; got:\n%s", out)
	}
	if strings.Contains(out, "re-run with --force") {
		t.Errorf("warn should not suggest --force; got:\n%s", out)
	}
	if !strings.Contains(out, "warning:") {
		t.Errorf("warn should start with 'warning:'; got:\n%s", out)
	}
	for _, p := range []string{"a.yaml", "b.yaml"} {
		if !strings.Contains(out, p) {
			t.Errorf("want %q in warning, got: %s", p, out)
		}
	}
}
