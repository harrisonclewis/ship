package ops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/harrisonclewis/ship/internal/config"
)

// sshBatchModeOption keeps ssh from asking questions on the terminal. Git's
// GIT_TERMINAL_PROMPT only covers credential helpers; ssh reads /dev/tty for
// host-key confirmation, which can draw over a TUI.
const sshBatchModeOption = "-o BatchMode=yes"

// gitEnvironment is the complete environment for a Git command that might
// reach a remote. It keeps the user's HOME, SSH agent and Git configuration
// while disabling credential prompts and SSH questions.
func gitEnvironment() []string {
	base := os.Environ()
	return mergeEnv(base, append(gitEnv(), gitSSHEnv(base, coreSSHCommand)...))
}

// gitSSHEnv returns the variables that make Git's SSH command non-interactive.
// A user's GIT_SSH_COMMAND or core.sshCommand is kept and given BatchMode; a
// bare GIT_SSH program cannot take arguments, so it is left untouched.
func gitSSHEnv(base []string, coreSSH func() string) []string {
	if command, ok := envValue(base, "GIT_SSH_COMMAND"); ok && strings.TrimSpace(command) != "" {
		return []string{"GIT_SSH_COMMAND=" + withSSHBatchMode(command)}
	}
	// Git prefers core.sshCommand over a bare GIT_SSH program.
	if command := strings.TrimSpace(coreSSH()); command != "" {
		return []string{"GIT_SSH_COMMAND=" + withSSHBatchMode(command)}
	}
	if program, ok := envValue(base, "GIT_SSH"); ok && strings.TrimSpace(program) != "" {
		return nil
	}
	return []string{"GIT_SSH_COMMAND=ssh " + sshBatchModeOption}
}

func withSSHBatchMode(command string) string {
	return strings.TrimRight(command, " ") + " " + sshBatchModeOption
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix), true
		}
	}
	return "", false
}

var (
	coreSSHCommandOnce sync.Once
	coreSSHCommandText string
)

// coreSSHCommand reads the user's global or system core.sshCommand. The
// repositories Ship caches are its own, so repository configuration is not
// consulted.
func coreSSHCommand() string {
	coreSSHCommandOnce.Do(func() {
		out, err := exec.Command("git", "config", "--get", "core.sshCommand").Output()
		if err != nil {
			return
		}
		coreSSHCommandText = strings.TrimSpace(string(out))
	})
	return coreSSHCommandText
}

// GitHostKeyError reports that Git could not verify a remote SSH host key
// before running the command. Host is the hostname, Port only when the source
// URL named one, and User the SSH user from the source (git by default).
type GitHostKeyError struct {
	Host    string
	User    string
	Port    string
	Changed bool
}

func (e *GitHostKeyError) Error() string {
	if e.Changed {
		return fmt.Sprintf("the SSH host key for %s has changed; check it manually before connecting, or use an https:// URL", e.displayHost())
	}
	return fmt.Sprintf("cannot verify the SSH host key for %s; run `ssh %s` in a terminal and confirm the fingerprint, or use an https:// URL",
		e.displayHost(), strings.Join(e.SSHArgs(), " "))
}

// Target is the user@host argument for ssh.
func (e *GitHostKeyError) Target() string {
	return e.User + "@" + e.Host
}

// SSHArgs is the argument list for an interactive ssh trust check.
func (e *GitHostKeyError) SSHArgs() []string {
	args := []string{"-T"}
	if e.Port != "" {
		args = append(args, "-p", e.Port)
	}
	return append(args, e.Target())
}

func (e *GitHostKeyError) displayHost() string {
	if e.Port == "" {
		return e.Host
	}
	return net.JoinHostPort(strings.Trim(e.Host, "[]"), e.Port)
}

// gitCommandError keeps git's stderr with the exit error. ExecRunner only
// returns "exit status 1", so the stderr is needed to recognise host-key
// failures.
type gitCommandError struct {
	err    error
	stderr string
}

func (e *gitCommandError) Error() string { return e.err.Error() }
func (e *gitCommandError) Unwrap() error { return e.err }

func gitError(err error, stderr string) error {
	if err == nil {
		return nil
	}
	return &gitCommandError{err: err, stderr: stderr}
}

// captureWriter tees stderr to the session writer and keeps a copy for error
// classification.
type captureWriter struct {
	w   io.Writer
	buf bytes.Buffer
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.buf.Write(p)
	if c.w == nil {
		return len(p), nil
	}
	return c.w.Write(p)
}

// wrapGitHostKeyError turns a git or ssh host-key failure into the typed error
// the TUI can offer to fix. host comes only from the app's configured Git
// source, never from command output or free text.
func wrapGitHostKeyError(err error, repo config.GitRepo) error {
	if err == nil {
		return nil
	}
	var typed *GitHostKeyError
	if errors.As(err, &typed) {
		return err
	}
	changed, ok := classifyHostKeyError(errorText(err))
	if !ok {
		return err
	}
	user, host, port := sshTarget(repo)
	return &GitHostKeyError{Host: host, User: user, Port: port, Changed: changed}
}

func errorText(err error) string {
	var commandErr *gitCommandError
	if errors.As(err, &commandErr) && commandErr.stderr != "" {
		return commandErr.stderr
	}
	return err.Error()
}

func classifyHostKeyError(text string) (changed bool, ok bool) {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"remote host identification has changed",
		"possible dns spoofing detected",
		"has changed and you have requested strict checking",
		"offending ",
	} {
		if strings.Contains(lower, marker) {
			return true, true
		}
	}
	for _, marker := range []string{
		"host key verification failed",
		"authenticity of host",
		"host key is known for",
		"is not known by any other names",
	} {
		if strings.Contains(lower, marker) {
			return false, true
		}
	}
	return false, false
}

func sshTarget(repo config.GitRepo) (user, host, port string) {
	user = repo.User
	if user == "" {
		user = "git"
	}
	host = repo.Host
	if h, p, err := net.SplitHostPort(repo.Host); err == nil {
		host, port = h, p
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return user, host, port
}
