package ops

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

type fakeLocalRunner struct {
	output   func(name string, args []string) ([]byte, error)
	download func(name string, args []string) ([]byte, error)
}

func (f *fakeLocalRunner) Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	if f.output != nil {
		return f.output(name, args)
	}
	return nil, nil
}

func (f *fakeLocalRunner) Download(ctx context.Context, dir string, env []string, w io.Writer, name string, args ...string) error {
	if f.download == nil {
		return errors.New("unexpected download")
	}
	data, err := f.download(name, args)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func TestParseGitSelector(t *testing.T) {
	cases := []struct {
		in    string
		kind  string
		value string
		asset string
		bad   bool
	}{
		{"", "default", "", "", false},
		{"commit:abc1234", "commit", "abc1234", "", false},
		{"commit:ABC1234", "commit", "abc1234", "", false},
		{"release:v1.2.3", "release", "v1.2.3", "", false},
		{"release:v1.2.3:hello_linux_amd64", "release-asset", "v1.2.3", "hello_linux_amd64", false},
		{"release:v1.2.3:../evil", "release-asset", "", "", true},
		{"release:v1.2.3:sha256sums", "release-asset", "", "", true},
		{"release:v1.2.3:a*b", "release-asset", "", "", true},
		{"release:v1.2.3:../evil:x", "release-asset", "", "", true},
		{"v1.2.3", "", "", "", true},
		{"commit:xyz", "", "", "", true},
	}
	for _, tc := range cases {
		got, err := parseGitSelector(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("%q was accepted", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got.kind != tc.kind || got.value != tc.value || got.asset != tc.asset {
			t.Errorf("%q -> %+v", tc.in, got)
		}
	}
}

func TestResolveGitRevisionDefaultBranchUsesFetchedRef(t *testing.T) {
	cache := t.TempDir()
	sha := strings.Repeat("a", 40)
	runner := &fakeLocalRunner{output: func(name string, args []string) ([]byte, error) {
		if name != "git" {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "ls-remote --symref origin HEAD"):
			return []byte("ref: refs/heads/main\tHEAD\n" + sha + "\tHEAD\n"), nil
		case strings.Contains(joined, "rev-parse --verify refs/heads/main^{commit}"):
			return []byte(sha + "\n"), nil
		default:
			t.Fatalf("unexpected git args %v", args)
			return nil, nil
		}
	}}
	op := New(&config.Config{}, Options{Local: runner})
	resolved, err := op.defaultBranchRevision(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Commit != sha || resolved.Ref != "branch:main" || resolved.ImageVersion != sha {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveGitReleaseAssetNeedsPublishedRelease(t *testing.T) {
	cache := t.TempDir()
	sha := strings.Repeat("b", 40)
	repo, err := config.ParseLocalSource("https://github.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeLocalRunner{output: func(name string, args []string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case name == "gh" && strings.Contains(joined, "/releases/tags/v1.2.3"):
			return []byte(`{"tag_name":"v1.2.3","draft":false,"assets":[{"id":7,"name":"hello_linux_amd64","size":3,"digest":"sha256:` + strings.Repeat("c", 64) + `"}]}`), nil
		case name == "git" && strings.Contains(joined, "refs/tags/v1.2.3^{commit}"):
			return []byte(sha + "\n"), nil
		default:
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		}
	}}
	op := New(&config.Config{}, Options{Local: runner})
	resolved, err := op.resolveGitRevision(context.Background(), cache, repo.Git, "release:v1.2.3:hello_linux_amd64")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Commit != sha || resolved.Tag != "v1.2.3" || resolved.Asset != "hello_linux_amd64" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if resolved.Ref != "release:v1.2.3:hello_linux_amd64" {
		t.Fatalf("ref = %q", resolved.Ref)
	}
	if !strings.HasPrefix(resolved.ImageVersion, "asset-") {
		t.Fatalf("image version = %q", resolved.ImageVersion)
	}
}

func TestReleaseAssetImageVersionBindsTagNameCommitAndDigest(t *testing.T) {
	base := releaseAssetImageVersion("v1", "app_linux_amd64", strings.Repeat("a", 40), strings.Repeat("b", 64))
	otherAsset := releaseAssetImageVersion("v1", "other_linux_amd64", strings.Repeat("a", 40), strings.Repeat("b", 64))
	otherDigest := releaseAssetImageVersion("v1", "app_linux_amd64", strings.Repeat("a", 40), strings.Repeat("c", 64))
	otherCommit := releaseAssetImageVersion("v1", "app_linux_amd64", strings.Repeat("b", 40), strings.Repeat("b", 64))
	if len(map[string]bool{base: true, otherAsset: true, otherDigest: true, otherCommit: true}) != 4 {
		t.Fatal("asset image versions collide")
	}
	if err := config.ValidateImageVersion(base); err != nil {
		t.Fatal(err)
	}
}

func TestDockerfileDeclaresBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	_ = os.WriteFile(path, []byte("FROM scratch\nARG VERSION\nARG BINARY=app\n"), 0o644)
	if !dockerfileDeclaresBinary(path) {
		t.Fatal("ARG BINARY not detected")
	}
	_ = os.WriteFile(path, []byte("FROM scratch\nARG VERSION\n"), 0o644)
	if dockerfileDeclaresBinary(path) {
		t.Fatal("missing ARG BINARY accepted")
	}
}

func TestNewerAvailableReleaseAndCommit(t *testing.T) {
	repo, err := config.ParseLocalSource("https://github.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("d", 40)
	runner := &fakeLocalRunner{output: func(name string, args []string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case name == "gh" && strings.Contains(joined, "release list"):
			return []byte(`[{"tagName":"v2.0.0"}]`), nil
		case name == "git" && strings.Contains(joined, "ls-remote --symref"):
			return []byte("ref: refs/heads/main\tHEAD\n" + sha + "\tHEAD\n"), nil
		default:
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		}
	}}
	op := New(&config.Config{}, Options{Local: runner})
	newer, latest, err := op.newerAvailable(context.Background(), repo.Git, sha, "release:v1.0.0")
	if err != nil || !newer || latest != "release:v2.0.0" {
		t.Fatalf("release check = %v %q %v", newer, latest, err)
	}
	newer, latest, err = op.newerAvailable(context.Background(), repo.Git, sha, "branch:main")
	if err != nil || newer || latest != "" {
		t.Fatalf("same commit check = %v %q %v", newer, latest, err)
	}
	// A failed lookup must not look like a newer release.
	failing := New(&config.Config{}, Options{Local: &fakeLocalRunner{output: func(name string, args []string) ([]byte, error) {
		return nil, errors.New("gh: authentication required")
	}}})
	if newer, _, err := failing.newerAvailable(context.Background(), repo.Git, sha, "release:v1.0.0"); err == nil || newer {
		t.Fatalf("auth error produced newer=%v err=%v", newer, err)
	}
	// No successful deploy: never badge.
	if newer, _, err := op.newerAvailable(context.Background(), repo.Git, "", ""); err != nil || newer {
		t.Fatalf("empty current produced newer=%v err=%v", newer, err)
	}
}

func TestEnsureGitCacheRejectsDifferentOrigin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-based cache path")
	}
	t.Setenv("HOME", t.TempDir())
	repo, err := config.ParseLocalSource("https://github.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	op := New(&config.Config{}, Options{})
	dir, err := op.repositoryDir("prod", repo.Git)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onOutput: func(c remote.Cmd) ([]byte, error) {
		if hasArg(c.Args, "remote.origin.url") {
			return []byte("https://github.com/other/app\n"), nil
		}
		t.Fatalf("unexpected command %v", c.Args)
		return nil, nil
	}}
	s := remote.NewSession("ops@host", false, io.Discard, io.Discard)
	s.Runner = r
	if _, err := op.ensureGitCache(context.Background(), s, "prod", repo.Git); err == nil {
		t.Fatal("cache with a different origin was accepted")
	}
}

func TestPublishedReleaseKeepsGHFailureDetail(t *testing.T) {
	repo, err := config.ParseLocalSource("https://github.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	op := New(&config.Config{}, Options{Local: &fakeLocalRunner{output: func(name string, args []string) ([]byte, error) {
		return nil, errors.New("gh: To use GitHub CLI in a GitHub Actions workflow, set the GH_TOKEN environment variable")
	}}})
	_, err = op.publishedRelease(context.Background(), repo.Git, "v1.2.3")
	if err == nil || !strings.Contains(err.Error(), "GH_TOKEN") {
		t.Fatalf("error lost gh detail: %v", err)
	}
}

func TestVerifyFileDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asset")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if err := verifyFileDigest(path, "sha256:"+sum); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileDigest(path, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("bad digest accepted")
	}
}

func TestChecksumForExactAsset(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "sums")
	hash := strings.Repeat("e", 64)
	if err := os.WriteFile(sums, []byte(hash+"  hello_linux_amd64\n"+hash+"  other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := checksumFor(sums, "hello_linux_amd64")
	if err != nil || got != hash {
		t.Fatalf("checksum = %q %v", got, err)
	}
	if _, err := checksumFor(sums, "hello"); err == nil {
		t.Fatal("prefix match accepted")
	}
}
