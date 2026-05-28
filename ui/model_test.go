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
