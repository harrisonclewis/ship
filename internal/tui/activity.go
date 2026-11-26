package tui

import (
	"bytes"
	"context"
	"sync"
	"time"
)

type runState int

const (
	runRunning runState = iota
	runSucceeded
	runFailed
)

func (s runState) String() string {
	switch s {
	case runRunning:
		return "running"
	case runSucceeded:
		return "succeeded"
	case runFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// syncBuffer is written by the operation goroutine while the UI reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Since returns output after the first n bytes. It also returns the new
// offset. If the buffer was reset the caller starts again at zero.
func (b *syncBuffer) Since(n int) (string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.buf.Bytes()
	if n < 0 || n > len(data) {
		n = 0
	}
	return string(data[n:]), len(data)
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

type activity struct {
	id      int
	req     Request
	label   string
	start   time.Time
	state   runState
	err     error
	result  Result
	out     *syncBuffer
	ctx     context.Context
	cancel  context.CancelFunc
	shown   int
	endTime time.Time
}

func (a *activity) elapsed(now time.Time) time.Duration {
	end := now
	if !a.endTime.IsZero() {
		end = a.endTime
	}
	return end.Sub(a.start)
}

type toastKind int

const (
	toastInfo toastKind = iota
	toastSuccess
	toastWarning
	toastError
)

type toast struct {
	kind toastKind
	msg  string
	seq  int
}

type runDoneMsg struct {
	id     int
	result Result
	err    error
}

type runTickMsg struct{}

type toastExpireMsg struct {
	seq int
}
