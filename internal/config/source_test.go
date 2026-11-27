package config

import (
	"strings"
	"testing"
)

func TestParseGitSourcePreservesTransport(t *testing.T) {
	tests := []struct {
		raw       string
		transport string
		scheme    string
		repoPath  string
		cacheKey  string
	}{
		{
			raw:       "git@github.com:acme/app.git",
			transport: "git@github.com:acme/app.git",
			scheme:    "scp",
			repoPath:  "acme/app",
			cacheKey:  "github.com/scp/git/acme/app",
		},
		{
			raw:       "ssh://git@github.com/acme/app.git",
			transport: "ssh://git@github.com/acme/app.git",
			scheme:    "ssh",
			repoPath:  "acme/app",
			cacheKey:  "github.com/ssh/git/acme/app",
		},
		{
			raw:       "https://github.com/acme/app.git",
			transport: "https://github.com/acme/app.git",
			scheme:    "https",
			repoPath:  "acme/app",
			cacheKey:  "github.com/https/acme/app",
		},
	}
	for _, tc := range tests {
		source, err := ParseLocalSource(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if !source.IsGit {
			t.Fatalf("%s: not detected as Git", tc.raw)
		}
		if source.Git.Transport != tc.transport {
			t.Errorf("%s: transport = %q, want %q", tc.raw, source.Git.Transport, tc.transport)
		}
		if source.Git.Scheme != tc.scheme || source.Git.RepoPath != tc.repoPath {
			t.Errorf("%s: parsed %+v", tc.raw, source.Git)
		}
		if got := source.Git.CacheKey(); got != tc.cacheKey {
			t.Errorf("%s: CacheKey = %q, want %q", tc.raw, got, tc.cacheKey)
		}
	}
}

func TestScpAndSSHAbsolutePathsStayDistinct(t *testing.T) {
	scp, err := ParseLocalSource("git@example.com:acme/app")
	if err != nil {
		t.Fatal(err)
	}
	ssh, err := ParseLocalSource("ssh://git@example.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	if scp.Git.CacheKey() == ssh.Git.CacheKey() {
		t.Fatalf("scp and ssh-absolute paths share cache key %q", scp.Git.CacheKey())
	}
}

func TestGitCacheKeyUsesFullRepositoryPath(t *testing.T) {
	a, err := ParseLocalSource("https://github.com/alice/app")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseLocalSource("https://github.com/bob/app")
	if err != nil {
		t.Fatal(err)
	}
	if a.Git.CacheKey() == b.Git.CacheKey() {
		t.Fatal("same basename under different owners shares a cache directory")
	}
}

func TestGitSourcesRejectUnsafeOrSecretInput(t *testing.T) {
	cases := []string{
		"https://user:supersecret@github.com/acme/app",
		"https://github.com/../app",
		"https://../app",
		"ssh://git@../app",
		"ssh://bad user@github.com/acme/app",
		"https://github.com/acme/app?token=supersecret",
		"ext::sh -c whoami",
		"file:///srv/git/app",
		"-oProxyCommand=whoami",
	}
	for _, raw := range cases {
		_, err := ParseLocalSource(raw)
		if err == nil {
			t.Errorf("%q was accepted", raw)
			continue
		}
		if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), raw) {
			t.Errorf("%q leaked into error %q", raw, err)
		}
	}
}

func TestPathSourcesStillValidate(t *testing.T) {
	source, err := ParseLocalSource("~/code/app")
	if err != nil {
		t.Fatal(err)
	}
	if source.IsGit || source.Path == "" {
		t.Fatalf("source = %+v", source)
	}
}

func TestSourcesAcceptsGitURLAndKeepsTransport(t *testing.T) {
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    sources:
      web: git@github.com:acme/app.git
`
	cfg, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Servers["prod"].Sources["web"]; got != "git@github.com:acme/app.git" {
		t.Fatalf("sources.web = %q", got)
	}
}
