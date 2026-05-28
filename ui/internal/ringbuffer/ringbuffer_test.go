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
