package ops

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// LocalRunner runs Git and GitHub CLI commands on this machine. It is a small
// test seam; the real implementation uses exec.
type LocalRunner interface {
	Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
	Download(ctx context.Context, dir string, env []string, w io.Writer, name string, args ...string) error
}

type execLocalRunner struct{}

func (execLocalRunner) Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = mergeEnv(os.Environ(), env)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			text = err.Error()
		}
		return stdout.Bytes(), fmt.Errorf("%s: %s", name, text)
	}
	return stdout.Bytes(), nil
}

func (execLocalRunner) Download(ctx context.Context, dir string, env []string, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = mergeEnv(os.Environ(), env)
	}
	var stderr bytes.Buffer
	cmd.Stdout = w
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("%s: %s", name, text)
	}
	return nil
}

// gitEnv disables interactive credential prompts so a background refresh or
// deploy cannot hang waiting for a terminal. SSH is made non-interactive
// separately in gitEnvironment.
func gitEnv() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"LC_ALL=C",
	}
}

// mergeEnv overlays extra variables on base, replacing an existing key instead
// of passing duplicate entries to the child.
func mergeEnv(base, extra []string) []string {
	override := make(map[string]bool, len(extra))
	for _, e := range extra {
		if k, _, ok := strings.Cut(e, "="); ok {
			override[k] = true
		}
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, e := range base {
		if k, _, ok := strings.Cut(e, "="); ok && override[k] {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}
