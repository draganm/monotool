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
