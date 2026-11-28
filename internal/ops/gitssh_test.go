package ops

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
)

func TestGitSSHEnvKeepsUserCommandNonInteractive(t *testing.T) {
	const core = "ssh -i /home/me/.ssh/id_ed25519"
	cases := []struct {
		name string
		env  []string
		core string
		want []string
	}{
		{
			name: "GIT_SSH_COMMAND is extended",
			env:  []string{"GIT_SSH_COMMAND=ssh -o ConnectTimeout=5"},
			core: core,
			want: []string{"GIT_SSH_COMMAND=ssh -o ConnectTimeout=5 -o BatchMode=yes"},
		},
		{
			name: "only GIT_SSH is left alone",
			env:  []string{"GIT_SSH=/usr/local/bin/ssh"},
			want: nil,
		},
		{
			name: "GIT_SSH does not hide core.sshCommand",
			env:  []string{"GIT_SSH=/usr/local/bin/ssh"},
			core: core,
			want: []string{"GIT_SSH_COMMAND=" + core + " -o BatchMode=yes"},
		},
		{
			name: "empty GIT_SSH_COMMAND falls through",
			env:  []string{"GIT_SSH_COMMAND="},
			core: core,
			want: []string{"GIT_SSH_COMMAND=" + core + " -o BatchMode=yes"},
		},
		{
			name: "core.sshCommand is extended",
			core: core,
			want: []string{"GIT_SSH_COMMAND=" + core + " -o BatchMode=yes"},
		},
		{
			name: "default falls back to ssh",
			want: []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes"},
		},
		{
			name: "GIT_SSH_COMMAND wins over GIT_SSH",
			env:  []string{"GIT_SSH=/usr/bin/ssh", "GIT_SSH_COMMAND=ssh -i key"},
			core: core,
			want: []string{"GIT_SSH_COMMAND=ssh -i key -o BatchMode=yes"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gitSSHEnv(tc.env, func() string { return tc.core })
			if len(got) != len(tc.want) {
				t.Fatalf("gitSSHEnv = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("gitSSHEnv = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestClassifyHostKeyError(t *testing.T) {
	cases := []struct {
		name    string
		stderr  string
		changed bool
		ok      bool
	}{
		{
			name: "unknown key in batch mode",
			stderr: "Cloning into '/tmp/cache'...\n" +
				"No ED25519 host key is known for github.com and you have requested strict checking.\n" +
				"Host key verification failed.\n" +
				"fatal: Could not read from remote repository.\n",
			ok: true,
		},
		{
			name: "unknown key during the authenticity prompt",
			stderr: "The authenticity of host 'github.com (140.82.121.4)' can't be established.\n" +
				"ED25519 key fingerprint is SHA256:abcdef.\n" +
				"Are you sure you want to continue connecting (yes/no/[fingerprint])? " +
				"Host key verification failed.\n",
			ok: true,
		},
		{
			name: "changed key",
			stderr: "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n" +
				"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n" +
				"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n" +
				"It is possible that someone is doing something nasty!\n" +
				"Offending ED25519 key in /home/me/.ssh/known_hosts:1\n" +
				"Host key for github.com has changed and you have requested strict checking.\n" +
				"Host key verification failed.\n",
			changed: true,
			ok:      true,
		},
		{
			name:   "authentication failure is not a host key error",
			stderr: "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n",
		},
		{
			name:   "unrelated failure",
			stderr: "fatal: repository 'https://github.com/acme/missing.git/' not found\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed, ok := classifyHostKeyError(tc.stderr)
			if changed != tc.changed || ok != tc.ok {
				t.Fatalf("classifyHostKeyError = (%v, %v), want (%v, %v)", changed, ok, tc.changed, tc.ok)
			}
		})
	}
}

func TestWrapGitHostKeyErrorNamesHostAndSshTarget(t *testing.T) {
	repo, err := config.ParseLocalSource("ssh://deploy@git.example.com:2222/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	gitErr := errors.New("git: No ED25519 host key is known for git.example.com and you have requested strict checking.\nHost key verification failed.\n")
	wrapped := wrapGitHostKeyError(gitErr, repo.Git)
	var hostKey *GitHostKeyError
	if !errors.As(wrapped, &hostKey) {
		t.Fatalf("error was not typed: %v", wrapped)
	}
	if hostKey.Host != "git.example.com" || hostKey.User != "deploy" || hostKey.Port != "2222" || hostKey.Changed {
		t.Fatalf("host key error = %+v", hostKey)
	}
	msg := hostKey.Error()
	for _, want := range []string{
		"git.example.com:2222",
		"ssh -T -p 2222 deploy@git.example.com",
		"confirm the fingerprint",
		"https://",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not contain %q", msg, want)
		}
	}
	if got := strings.Join(hostKey.SSHArgs(), " "); got != "-T -p 2222 deploy@git.example.com" {
		t.Fatalf("SSHArgs = %q", got)
	}
}

func TestWrapGitHostKeyErrorChangedKeyOffersNoTrust(t *testing.T) {
	repo, err := config.ParseLocalSource("git@github.com:acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	gitErr := errors.New("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!\nHost key verification failed.\n")
	wrapped := wrapGitHostKeyError(gitErr, repo.Git)
	var hostKey *GitHostKeyError
	if !errors.As(wrapped, &hostKey) {
		t.Fatalf("error was not typed: %v", wrapped)
	}
	if !hostKey.Changed || hostKey.Host != "github.com" || hostKey.User != "git" || hostKey.Port != "" {
		t.Fatalf("host key error = %+v", hostKey)
	}
	if strings.Contains(hostKey.Error(), "confirm the fingerprint") || strings.Contains(hostKey.Error(), "ssh -T") {
		t.Fatalf("changed-key error offers trust: %q", hostKey.Error())
	}
	if !strings.Contains(hostKey.Error(), "has changed") {
		t.Fatalf("changed-key error is unclear: %q", hostKey.Error())
	}
}

func TestWrapGitHostKeyErrorUsesCapturedStderr(t *testing.T) {
	repo, err := config.ParseLocalSource("git@github.com:acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	// ExecRunner returns only "exit status 1"; the captured stderr is what
	// identifies the failure.
	wrapped := wrapGitHostKeyError(gitError(errors.New("exit status 1"), "Host key verification failed.\n"), repo.Git)
	var hostKey *GitHostKeyError
	if !errors.As(wrapped, &hostKey) {
		t.Fatalf("captured stderr was not classified: %v", wrapped)
	}
	if hostKey.Host != "github.com" || hostKey.Changed {
		t.Fatalf("host key error = %+v", hostKey)
	}
}

func TestDeploySummaryErrorKeepsTypedHostKeyFailure(t *testing.T) {
	hostErr := &GitHostKeyError{Host: "github.com", User: "git"}
	summary := DeploySummary{
		Server: "prod",
		Failed: []DeployFailure{{App: "web", Err: fmt.Errorf("clone repository into the cache: %w", hostErr)}},
	}
	var found *GitHostKeyError
	if !errors.As(summary.summaryError(), &found) {
		t.Fatal("deploy-all summary dropped the typed host key error")
	}
}

func TestWrapGitHostKeyErrorLeavesOtherFailuresAlone(t *testing.T) {
	repo, err := config.ParseLocalSource("https://github.com/acme/app.git")
	if err != nil {
		t.Fatal(err)
	}
	original := errors.New("fatal: repository not found")
	if got := wrapGitHostKeyError(original, repo.Git); got != original {
		t.Fatalf("wrapped unrelated error: %v", got)
	}
	if got := wrapGitHostKeyError(nil, repo.Git); got != nil {
		t.Fatalf("wrapped nil: %v", got)
	}
}
