package ops

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/harrisonclewis/ship/internal/remote"
)

func TestCaptureAgentStderrMarksConfigConflict(t *testing.T) {
	s := &remote.Session{Err: io.Discard}
	err := captureAgentStderr(s, func() error {
		fmt.Fprint(s.Err, "app 'web' changed on the server since you loaded it\n")
		return errors.New("exit status 1")
	})
	if !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("error = %v, want ErrConfigConflict", err)
	}
}

func TestCaptureAgentStderrPassesOtherErrorsThrough(t *testing.T) {
	s := &remote.Session{Err: io.Discard}
	want := errors.New("ssh: connection refused")
	err := captureAgentStderr(s, func() error {
		fmt.Fprint(s.Err, "sudo: a password is required\n")
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if errors.Is(err, ErrConfigConflict) {
		t.Fatal("unrelated error was marked as a conflict")
	}
}
