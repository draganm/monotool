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
