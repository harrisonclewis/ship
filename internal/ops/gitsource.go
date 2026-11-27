package ops

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

const (
	gitCommitLimit  = 50
	gitReleaseLimit = 30
	gitCheckTimeout = 8 * time.Second
)

// GitCommit is one entry in the deploy picker's Commits tab.
type GitCommit struct {
	SHA     string
	Short   string
	Subject string
	Time    time.Time
}

// GitRelease is one published GitHub release in the deploy picker.
type GitRelease struct {
	Tag       string
	Name      string
	Published time.Time
	Latest    bool
}

// GitAsset is one downloadable non-checksum asset of a published release.
type GitAsset struct {
	Name   string
	Size   int64
	Digest string
}

type gitSelector struct {
	kind  string // default, commit, release, release-asset
	value string // commit SHA or release tag
	asset string
}

type gitResolution struct {
	Commit       string
	Ref          string
	ImageVersion string
	Tag          string
	Asset        string
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

var (
	gitCommitSHA = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	gitTagRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+@-]{0,127}$`)
	gitAssetRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,199}$`)
)

func parseGitSelector(version string) (gitSelector, error) {
	version = strings.TrimSpace(version)
	switch {
	case version == "":
		return gitSelector{kind: "default"}, nil
	case strings.HasPrefix(version, "commit:"):
		sha := strings.TrimPrefix(version, "commit:")
		if !gitCommitSHA.MatchString(sha) {
			return gitSelector{}, fmt.Errorf("commit: wants a Git commit SHA or a prefix of at least 7 hexadecimal characters")
		}
		return gitSelector{kind: "commit", value: strings.ToLower(sha)}, nil
	case strings.HasPrefix(version, "release:"):
		rest := strings.TrimPrefix(version, "release:")
		tag, asset, hasAsset := strings.Cut(rest, ":")
		if !validGitTag(tag) {
			return gitSelector{}, fmt.Errorf("release: wants a published release tag")
		}
		if hasAsset {
			if !validGitAsset(asset) {
				return gitSelector{}, fmt.Errorf("invalid release asset name")
			}
			return gitSelector{kind: "release-asset", value: tag, asset: asset}, nil
		}
		return gitSelector{kind: "release", value: tag}, nil
	default:
		return gitSelector{}, fmt.Errorf("for a Git repository source, use commit:<sha>, release:<tag>, or no version for the default branch")
	}
}

func validGitAsset(name string) bool {
	if !gitAssetRE.MatchString(name) || strings.HasPrefix(name, "-") ||
		strings.Contains(name, "..") || isChecksumAsset(name) {
		return false
	}
	return true
}

func isChecksumAsset(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "sha256sums", "sha256sums.txt", "checksums.txt", "checksum.txt", "sha256sum", "sha512sums", "sha512sums.txt":
		return true
	}
	for _, suffix := range []string{".sha256", ".sha256sum", ".sha512", ".sha512sum", ".md5", ".checksum", ".checksums"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func validGitTag(tag string) bool {
	if !gitTagRE.MatchString(tag) || strings.HasPrefix(tag, "-") || strings.Contains(tag, "..") ||
		strings.Contains(tag, "@{") || strings.HasSuffix(tag, ".") || strings.HasSuffix(tag, "/") ||
		strings.HasSuffix(tag, ".lock") {
		return false
	}
	return true
}

func (o *Ops) gitSource(server, app string) (config.GitRepo, bool, error) {
	srv, err := o.Server(server)
	if err != nil {
		return config.GitRepo{}, false, err
	}
	value := srv.Sources[app]
	if value == "" {
		return config.GitRepo{}, false, nil
	}
	repo, ok := config.GitSource(value)
	if !ok {
		return config.GitRepo{}, false, nil
	}
	return repo, true, nil
}

func (o *Ops) repositoryDir(server string, repo config.GitRepo) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ship", "servers", server, "repositories", filepath.FromSlash(repo.CacheKey())), nil
}

func (o *Ops) buildRoot(server string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ship", "servers", server, "builds"), nil
}

// ensureGitCache creates or updates the bare mirror. Existing caches are
// verified against the repository they were created from before any fetch.
func (o *Ops) ensureGitCache(ctx context.Context, s *remote.Session, server string, repo config.GitRepo) (string, error) {
	dir, err := o.repositoryDir(server, repo)
	if err != nil {
		return "", err
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "$ git clone --mirror -- %s %s\n", remote.ShellQuote(repo.Transport), remote.ShellQuote(dir))
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	release, err := acquireGitLock(ctx, dir+".lock")
	if err != nil {
		return "", err
	}
	defer release()

	if fi, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := gitSessionRun(ctx, s, "clone", "--mirror", "--", repo.Transport, dir); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("clone repository into the cache: %w", err)
		}
	} else if err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", fmt.Errorf("repository cache %s is not a directory", dir)
	} else {
		out, err := gitSessionOutput(ctx, s, "-C", dir, "config", "--get", "remote.origin.url")
		if err != nil {
			return "", fmt.Errorf("repository cache %s is not a usable Git repository", dir)
		}
		if strings.TrimSpace(string(out)) != repo.Transport {
			return "", fmt.Errorf("repository cache %s belongs to a different remote; remove it or use a different source", dir)
		}
		if err := gitSessionRun(ctx, s, "-C", dir, "fetch", "--prune", "origin",
			"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
			return "", fmt.Errorf("fetch repository: %w", err)
		}
	}
	return dir, nil
}

func acquireGitLock(ctx context.Context, path string) (func(), error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if fi, statErr := os.Stat(path); statErr == nil && time.Since(fi.ModTime()) > 10*time.Minute {
			_ = os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func gitSessionRun(ctx context.Context, s *remote.Session, args ...string) error {
	return s.Runner.Run(remote.Cmd{
		Path:    "git",
		Args:    args,
		Context: ctx,
		Stdout:  s.Out,
		Stderr:  s.Err,
		Env:     gitEnvironment(),
	})
}

func gitSessionOutput(ctx context.Context, s *remote.Session, args ...string) ([]byte, error) {
	return s.Runner.Output(remote.Cmd{
		Path:    "git",
		Args:    args,
		Context: ctx,
		Stderr:  s.Err,
		Env:     gitEnvironment(),
	})
}

// gitEnvironment keeps the user's HOME, SSH agent and Git configuration while
// disabling terminal credential prompts.
func gitEnvironment() []string {
	return mergeEnv(os.Environ(), gitEnv())
}

// resolveGitRevision turns a deploy version selector into a commit and a
// typed ref. The cache must have been fetched first.
func (o *Ops) resolveGitRevision(ctx context.Context, cache string, repo config.GitRepo, version string) (gitResolution, error) {
	sel, err := parseGitSelector(version)
	if err != nil {
		return gitResolution{}, err
	}
	switch sel.kind {
	case "commit":
		out, err := o.gitOutput(ctx, nil, "-C", cache, "rev-parse", "--verify", sel.value+"^{commit}")
		if err != nil {
			return gitResolution{}, fmt.Errorf("commit %s was not found in the repository", sel.value)
		}
		commit := strings.TrimSpace(string(out))
		return gitResolution{Commit: commit, Ref: "commit:" + shortSHA(commit, 8), ImageVersion: commit}, nil
	case "release", "release-asset":
		release, err := o.publishedRelease(ctx, repo, sel.value)
		if err != nil {
			return gitResolution{}, err
		}
		if sel.kind == "release-asset" {
			if _, ok := releaseAsset(release, sel.asset); !ok {
				return gitResolution{}, fmt.Errorf("release %s has no asset %s", sel.value, sel.asset)
			}
		}
		out, err := o.gitOutput(ctx, nil, "-C", cache, "rev-parse", "--verify", "refs/tags/"+sel.value+"^{commit}")
		if err != nil {
			return gitResolution{}, fmt.Errorf("release tag %s was not found after fetching; push the tag and fetch again", sel.value)
		}
		commit := strings.TrimSpace(string(out))
		resolved := gitResolution{
			Commit:       commit,
			Ref:          "release:" + sel.value,
			ImageVersion: commit,
			Tag:          sel.value,
		}
		if sel.kind == "release-asset" {
			resolved.Ref += ":" + sel.asset
			resolved.Asset = sel.asset
			resolved.ImageVersion = releaseAssetImageVersion(sel.value, sel.asset, commit, "")
		}
		return resolved, nil
	default:
		return o.defaultBranchRevision(ctx, cache)
	}
}

func (o *Ops) defaultBranchRevision(ctx context.Context, cache string) (gitResolution, error) {
	out, err := o.gitOutput(ctx, nil, "-C", cache, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return gitResolution{}, fmt.Errorf("resolve the remote default branch: %w", err)
	}
	branch := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ref: refs/heads/") && strings.HasSuffix(line, "\tHEAD") {
			branch = strings.TrimSuffix(strings.TrimPrefix(line, "ref: refs/heads/"), "\tHEAD")
			break
		}
	}
	if branch == "" {
		return gitResolution{}, fmt.Errorf("the remote did not report a default branch")
	}
	// Resolve the branch we just fetched rather than the SHA advertised by a
	// separate round trip: the remote may advance between the fetch and the
	// advertisement.
	out, err = o.gitOutput(ctx, nil, "-C", cache, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		// The branch appeared or moved after the fetch; fetch and retry once.
		if _, fetchErr := o.local.Output(ctx, "", gitEnv(), "git", "-C", cache, "fetch", "--prune", "origin",
			"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); fetchErr != nil {
			return gitResolution{}, fmt.Errorf("fetch the default branch: %w", fetchErr)
		}
		out, err = o.gitOutput(ctx, nil, "-C", cache, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
		if err != nil {
			return gitResolution{}, fmt.Errorf("the fetched default branch %s was not found", branch)
		}
	}
	commit := strings.TrimSpace(string(out))
	return gitResolution{Commit: commit, Ref: "branch:" + branch, ImageVersion: commit}, nil
}

func (o *Ops) publishedRelease(ctx context.Context, repo config.GitRepo, tag string) (ghRelease, error) {
	owner, name, ok := repo.GitHubOwnerRepo()
	if !ok {
		return ghRelease{}, fmt.Errorf("release: is only supported for github.com repositories")
	}
	endpoint := "repos/" + owner + "/" + name + "/releases/tags/" + url.PathEscape(tag)
	out, err := o.ghOutput(ctx, "api", "--hostname", "github.com", endpoint)
	if err != nil {
		return ghRelease{}, fmt.Errorf("look up GitHub release %s: %w", tag, err)
	}
	var release ghRelease
	if err := json.Unmarshal(out, &release); err != nil || release.TagName != tag {
		return ghRelease{}, fmt.Errorf("GitHub returned an unexpected release record for %s", tag)
	}
	if release.Draft {
		return ghRelease{}, fmt.Errorf("release %s is a draft; publish it before deploying", tag)
	}
	return release, nil
}

func releaseAsset(release ghRelease, name string) (GitAsset, bool) {
	for _, a := range release.Assets {
		if a.Name == name {
			return GitAsset{Name: a.Name, Size: a.Size, Digest: a.Digest}, true
		}
	}
	return GitAsset{}, false
}

// GitCommits lists recent commits on the repository's default branch. It
// fetches first so the picker is current.
func (o *Ops) GitCommits(ctx context.Context, server, app string) ([]GitCommit, error) {
	repo, ok, err := o.gitSource(server, app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("app %s has no Git repository source", app)
	}
	if o.opts.DryRun {
		return nil, nil
	}
	s, _, err := o.session(ctx, server)
	if err != nil {
		return nil, err
	}
	cache, err := o.ensureGitCache(ctx, s, server, repo)
	if err != nil {
		return nil, err
	}
	resolved, err := o.defaultBranchRevision(ctx, cache)
	if err != nil {
		return nil, err
	}
	out, err := o.gitOutput(ctx, nil, "-C", cache, "log", "-n", fmt.Sprint(gitCommitLimit), "--format=%H%x00%h%x00%cI%x00%s", resolved.Commit)
	if err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}
	commits := make([]GitCommit, 0, gitCommitLimit)
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) != 4 {
			continue
		}
		when, _ := time.Parse(time.RFC3339, parts[2])
		commits = append(commits, GitCommit{SHA: parts[0], Short: parts[1], Subject: parts[3], Time: when})
	}
	return commits, nil
}

// GitReleases lists published GitHub releases. Drafts are excluded.
func (o *Ops) GitReleases(ctx context.Context, server, app string) ([]GitRelease, error) {
	repo, ok, err := o.gitSource(server, app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("app %s has no Git repository source", app)
	}
	owner, name, ok := repo.GitHubOwnerRepo()
	if !ok {
		return nil, fmt.Errorf("releases are only listed for github.com repositories")
	}
	if o.opts.DryRun {
		return nil, nil
	}
	out, err := o.ghOutput(ctx, "release", "list", "--repo", "github.com/"+owner+"/"+name,
		"--limit", fmt.Sprint(gitReleaseLimit), "--exclude-drafts",
		"--json", "tagName,name,publishedAt,isLatest")
	if err != nil {
		return nil, fmt.Errorf("list GitHub releases: %w", err)
	}
	var raw []struct {
		TagName     string `json:"tagName"`
		Name        string `json:"name"`
		PublishedAt string `json:"publishedAt"`
		IsLatest    bool   `json:"isLatest"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("read GitHub releases: %w", err)
	}
	releases := make([]GitRelease, 0, len(raw))
	for _, r := range raw {
		when, _ := time.Parse(time.RFC3339, r.PublishedAt)
		releases = append(releases, GitRelease{Tag: r.TagName, Name: r.Name, Published: when, Latest: r.IsLatest})
	}
	return releases, nil
}

// GitReleaseAssets lists the downloadable, non-checksum assets of one
// published GitHub release.
func (o *Ops) GitReleaseAssets(ctx context.Context, server, app, tag string) ([]GitAsset, error) {
	repo, ok, err := o.gitSource(server, app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("app %s has no Git repository source", app)
	}
	if !validGitTag(tag) {
		return nil, fmt.Errorf("invalid release tag")
	}
	if o.opts.DryRun {
		return nil, nil
	}
	release, err := o.publishedRelease(ctx, repo, tag)
	if err != nil {
		return nil, err
	}
	assets := make([]GitAsset, 0, len(release.Assets))
	for _, a := range release.Assets {
		if isChecksumAsset(a.Name) || !validGitAsset(a.Name) {
			continue
		}
		assets = append(assets, GitAsset{Name: a.Name, Size: a.Size, Digest: a.Digest})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	return assets, nil
}

// newerAvailable reports whether the running identity is behind the remote.
// A release identity asks about published releases; a commit identity asks
// about the remote default branch. Errors (offline, auth) are never treated
// as "newer".
func (o *Ops) newerAvailable(ctx context.Context, repo config.GitRepo, current, currentRef string) (bool, string, error) {
	if current == "" {
		return false, "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, gitCheckTimeout)
	defer cancel()
	if tag, ok := releaseTagFromRef(currentRef); ok {
		owner, name, github := repo.GitHubOwnerRepo()
		if !github {
			return false, "", fmt.Errorf("release checks need a github.com repository")
		}
		out, err := o.ghOutput(ctx, "release", "list", "--repo", "github.com/"+owner+"/"+name,
			"--limit", "1", "--exclude-drafts", "--json", "tagName")
		if err != nil {
			return false, "", err
		}
		var raw []struct {
			TagName string `json:"tagName"`
		}
		if err := json.Unmarshal(out, &raw); err != nil || len(raw) == 0 {
			return false, "", fmt.Errorf("read the latest release")
		}
		latest := raw[0].TagName
		if latest != "" && latest != tag {
			return true, "release:" + latest, nil
		}
		return false, "", nil
	}
	out, err := o.gitOutput(ctx, nil, "ls-remote", "--symref", repo.Transport, "HEAD")
	if err != nil {
		return false, "", err
	}
	sha := ""
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "HEAD" && gitCommitSHA.MatchString(fields[0]) {
			sha = fields[0]
			break
		}
	}
	if sha == "" {
		return false, "", fmt.Errorf("the remote did not report HEAD")
	}
	if sha != current {
		return true, shortSHA(sha, 7), nil
	}
	return false, "", nil
}

func (o *Ops) gitOutput(ctx context.Context, s *remote.Session, args ...string) ([]byte, error) {
	if s != nil {
		return gitSessionOutput(ctx, s, args...)
	}
	return o.local.Output(ctx, "", gitEnv(), "git", args...)
}

func (o *Ops) ghOutput(ctx context.Context, args ...string) ([]byte, error) {
	return o.local.Output(ctx, "", []string{
		"GH_PROMPT_DISABLED=1",
		"GH_NO_UPDATE_NOTIFIER=1",
	}, "gh", args...)
}

func releaseTagFromRef(ref string) (string, bool) {
	rest, ok := strings.CutPrefix(ref, "release:")
	if !ok {
		return "", false
	}
	tag, _, _ := strings.Cut(rest, ":")
	if tag == "" {
		return "", false
	}
	return tag, true
}

// releaseAssetImageVersion binds the image identity to the release tag, the
// asset name, the source checkout commit and the verified payload digest. A
// replaced asset under the same name, or a moved tag, therefore produces a new
// image instead of overwriting the rollback tag.
func releaseAssetImageVersion(tag, asset, commit, payloadSHA string) string {
	sum := sha256.Sum256([]byte(tag + "\x00" + asset + "\x00" + commit + "\x00" + payloadSHA))
	return "asset-" + hex.EncodeToString(sum[:])
}

func shortSHA(sha string, n int) string {
	if len(sha) <= n {
		return sha
	}
	return sha[:n]
}

// buildGitImage clones a resolved commit into a private build directory,
// runs the app's approved build script and docker buildx there, and tags the
// image with the full commit SHA.
func (o *Ops) buildGitImage(ctx context.Context, s *remote.Session, srv *config.Server, name string, app *config.App, resolved gitResolution, cache string) error {
	if err := checkLocalBuild(s, name); err != nil {
		return err
	}
	buildDir, cleanup, err := o.checkoutGitBuild(ctx, s, srv, name, resolved, cache)
	if err != nil {
		return err
	}
	defer cleanup()
	if !s.DryRun {
		if fi, err := os.Stat(filepath.Join(buildDir, "Dockerfile")); err != nil || fi.IsDir() {
			return fmt.Errorf("the repository at %s has no Dockerfile", shortSHA(resolved.Commit, 8))
		}
	}
	if err := o.runBuildCommands(s, srv, name, app, buildDir); err != nil {
		return err
	}
	ref := srv.ImageRef(name, resolved.ImageVersion)
	return o.dockerBuild(s, srv, ref, buildDir, "")
}

// buildReleaseAssetImage builds an image from a verified release asset at the
// release tag's checkout, using Dockerfile.release when the checkout has one.
func (o *Ops) buildReleaseAssetImage(ctx context.Context, s *remote.Session, srv *config.Server, name string, resolved gitResolution, repo config.GitRepo, cache string) (string, error) {
	if err := checkLocalBuild(s, name); err != nil {
		return "", err
	}
	if s.DryRun {
		owner, repoName, _ := repo.GitHubOwnerRepo()
		fmt.Fprintf(s.Out, "$ gh api --hostname github.com repos/%s/%s/releases/tags/%s\n", owner, repoName, url.PathEscape(resolved.Tag))
		fmt.Fprintf(s.Out, "$ gh api --hostname github.com -H 'Accept: application/octet-stream' repos/%s/%s/releases/assets/<id-of-%s> > <asset>\n",
			owner, repoName, resolved.Asset)
		fmt.Fprintf(s.Out, "# verify the asset digest or release checksums, then copy it into the build as %s\n", "ship-release-binary")
		buildDir, cleanup, err := o.checkoutGitBuild(ctx, s, srv, name, resolved, cache)
		if err != nil {
			return "", err
		}
		defer cleanup()
		fmt.Fprintf(s.Out, "# would use Dockerfile.release when present, otherwise a Dockerfile declaring ARG BINARY\n")
		args := []string{"--build-arg", "VERSION=" + resolved.Tag, "--build-arg", "BINARY=ship-release-binary"}
		if err := o.dockerBuild(s, srv, srv.ImageRef(name, resolved.ImageVersion), buildDir, "", args...); err != nil {
			return "", err
		}
		return "asset-dry-run", nil
	}
	buildRoot, err := o.buildRoot(srv.Name)
	if err != nil {
		return "", err
	}
	stageDir := filepath.Join(buildRoot, name, "download-"+randomSuffix())
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return "", err
	}
	defer os.RemoveAll(stageDir)
	release, err := o.publishedRelease(ctx, repo, resolved.Tag)
	if err != nil {
		return "", err
	}
	assetPath, payloadSHA, err := o.downloadVerifiedAsset(ctx, repo, release, resolved.Asset, stageDir, s)
	if err != nil {
		return "", err
	}
	buildDir, cleanup, err := o.checkoutGitBuild(ctx, s, srv, name, resolved, cache)
	if err != nil {
		return "", err
	}
	defer cleanup()

	const binaryName = "ship-release-binary"
	dockerfile := "Dockerfile"
	releaseDockerfile := filepath.Join(buildDir, "Dockerfile.release")
	rootDockerfile := filepath.Join(buildDir, "Dockerfile")
	if fi, err := os.Lstat(releaseDockerfile); err == nil {
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("Dockerfile.release must be a regular file")
		}
		dockerfile = "Dockerfile.release"
	} else if fi, err := os.Lstat(rootDockerfile); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("the repository at %s has no Dockerfile.release or Dockerfile", shortSHA(resolved.Commit, 8))
	} else if !dockerfileDeclaresBinary(rootDockerfile) {
		return "", fmt.Errorf("the repository's Dockerfile does not declare ARG BINARY; add a Dockerfile.release that consumes the release asset, or add ARG BINARY to Dockerfile")
	}
	dest := filepath.Join(buildDir, binaryName)
	if err := os.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := copyRegularFile(assetPath, dest); err != nil {
		return "", fmt.Errorf("stage the release asset for the build: %w", err)
	}
	fmt.Fprintf(s.Out, "using verified asset %s for %s\n", resolved.Asset, resolved.Tag)

	imageVersion := releaseAssetImageVersion(resolved.Tag, resolved.Asset, resolved.Commit, payloadSHA)
	args := []string{"--build-arg", "VERSION=" + resolved.Tag, "--build-arg", "BINARY=" + binaryName}
	if err := o.dockerBuild(s, srv, srv.ImageRef(name, imageVersion), buildDir, dockerfile, args...); err != nil {
		return "", err
	}
	return imageVersion, nil
}

func dockerfileDeclaresBinary(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "ARG") {
			continue
		}
		for _, arg := range fields[1:] {
			if arg == "BINARY" || strings.HasPrefix(arg, "BINARY=") {
				return true
			}
		}
	}
	return false
}

// checkoutGitBuild makes an isolated build directory from the cache at a
// resolved commit. The returned cleanup is safe to defer immediately.
func (o *Ops) checkoutGitBuild(ctx context.Context, s *remote.Session, srv *config.Server, name string, resolved gitResolution, cache string) (string, func(), error) {
	buildRoot, err := o.buildRoot(srv.Name)
	if err != nil {
		return "", func() {}, err
	}
	buildDir := filepath.Join(buildRoot, name, resolved.Commit+"-"+randomSuffix())
	if s.DryRun {
		fmt.Fprintf(s.Out, "$ git clone --no-hardlinks --no-checkout -- %s %s\n", remote.ShellQuote(cache), remote.ShellQuote(buildDir))
		fmt.Fprintf(s.Out, "$ git -C %s checkout --detach %s\n", remote.ShellQuote(buildDir), remote.ShellQuote(resolved.Commit))
		return buildDir, func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(buildDir), 0o700); err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(buildDir) }
	if err := gitSessionRun(ctx, s, "clone", "--no-hardlinks", "--no-checkout", "--", cache, buildDir); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("prepare the build directory: %w", err)
	}
	if err := gitSessionRun(ctx, s, "-C", buildDir, "checkout", "--detach", resolved.Commit); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("check out %s: %w", shortSHA(resolved.Commit, 8), err)
	}
	_ = gitSessionRun(ctx, s, "-C", buildDir, "remote", "remove", "origin")
	return buildDir, cleanup, nil
}

func (o *Ops) dockerBuild(s *remote.Session, srv *config.Server, ref, buildDir, dockerfile string, extra ...string) error {
	arch := srv.Arch
	if arch == "" {
		arch = config.DefaultArch
	}
	args := []string{"buildx", "build", "--platform", arch, "--tag", ref, "--load"}
	if dockerfile != "" && dockerfile != "Dockerfile" {
		args = append(args, "-f", filepath.Join(buildDir, dockerfile))
	}
	args = append(args, extra...)
	args = append(args, buildDir)
	if err := s.Local(append([]string{"docker"}, args...)...); err != nil {
		return fmt.Errorf("docker buildx: %w", err)
	}
	fmt.Fprintf(s.Out, "built %s\n", ref)
	return nil
}

func (o *Ops) downloadVerifiedAsset(ctx context.Context, repo config.GitRepo, release ghRelease, name, stageDir string, s *remote.Session) (string, string, error) {
	asset, ok := releaseAsset(release, name)
	if !ok {
		return "", "", fmt.Errorf("release %s has no asset %s", release.TagName, name)
	}
	if !validGitAsset(name) {
		return "", "", fmt.Errorf("invalid release asset name")
	}
	dest := filepath.Join(stageDir, "asset")
	if s.DryRun {
		fmt.Fprintf(s.Out, "$ gh api --hostname github.com -H 'Accept: application/octet-stream' repos/%s/%s/releases/assets/%d > %s\n",
			ownerOf(repo), repoOf(repo), assetID(release, name), remote.ShellQuote(dest))
		return dest, "", nil
	}
	if err := o.downloadAssetFile(ctx, repo, release, name, dest); err != nil {
		return "", "", err
	}
	fi, err := os.Stat(dest)
	if err != nil {
		return "", "", err
	}
	if asset.Size > 0 && fi.Size() != asset.Size {
		_ = os.Remove(dest)
		return "", "", fmt.Errorf("downloaded asset %s has size %d, want %d", name, fi.Size(), asset.Size)
	}
	if asset.Digest != "" {
		if err := verifyFileDigest(dest, asset.Digest); err != nil {
			_ = os.Remove(dest)
			return "", "", fmt.Errorf("asset %s failed GitHub digest verification", name)
		}
		payload, err := fileSHA256(dest)
		if err != nil {
			return "", "", err
		}
		return dest, payload, nil
	}
	// Older GitHub responses have no digest. Fall back to the release's
	// SHA256SUMS/checksums.txt, as release-asset apps already do.
	checksumDir := filepath.Join(stageDir, "checksums")
	if err := os.MkdirAll(checksumDir, 0o700); err != nil {
		return "", "", err
	}
	for i, sumName := range checksumAssetNames(release) {
		sumPath := filepath.Join(checksumDir, fmt.Sprintf("sums-%d", i))
		if err := o.downloadAssetFile(ctx, repo, release, sumName, sumPath); err != nil {
			continue
		}
		want, err := checksumFor(sumPath, name)
		if err != nil {
			continue
		}
		got, err := fileSHA256(dest)
		if err != nil {
			return "", "", err
		}
		if !strings.EqualFold(got, want) {
			_ = os.Remove(dest)
			return "", "", fmt.Errorf("asset %s failed checksum verification", name)
		}
		return dest, got, nil
	}
	_ = os.Remove(dest)
	return "", "", fmt.Errorf("asset %s has no GitHub digest or release checksum; refusing to build it", name)
}

func (o *Ops) downloadAssetFile(ctx context.Context, repo config.GitRepo, release ghRelease, name, dest string) error {
	id := assetID(release, name)
	if id == 0 {
		return fmt.Errorf("release %s has no asset %s", release.TagName, name)
	}
	owner, repoName, ok := repo.GitHubOwnerRepo()
	if !ok {
		return fmt.Errorf("release assets need a github.com repository")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	okWrite := false
	defer func() {
		_ = f.Close()
		if !okWrite {
			_ = os.Remove(dest)
		}
	}()
	endpoint := fmt.Sprintf("repos/%s/%s/releases/assets/%d", owner, repoName, id)
	if err := o.local.Download(ctx, "", ghEnvironment(), f, "gh", "api", "--hostname", "github.com",
		"-H", "Accept: application/octet-stream", endpoint); err != nil {
		return fmt.Errorf("download asset %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	okWrite = true
	return nil
}

func checksumAssetNames(release ghRelease) []string {
	var names []string
	for _, preferred := range []string{"SHA256SUMS", "checksums.txt"} {
		for _, a := range release.Assets {
			if a.Name == preferred {
				names = append(names, a.Name)
			}
		}
	}
	for _, a := range release.Assets {
		if isChecksumAsset(a.Name) && !slices.Contains(names, a.Name) {
			names = append(names, a.Name)
		}
	}
	return names
}

func assetID(release ghRelease, name string) int64 {
	for _, a := range release.Assets {
		if a.Name == name {
			return a.ID
		}
	}
	return 0
}

func verifyFileDigest(path, digest string) error {
	algo, want, ok := strings.Cut(digest, ":")
	if !ok {
		return fmt.Errorf("unsupported digest")
	}
	var got string
	switch strings.ToLower(algo) {
	case "sha256":
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		got = sum
	case "sha512":
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha512.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		got = hex.EncodeToString(h.Sum(nil))
	default:
		return fmt.Errorf("unsupported digest algorithm")
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("digest mismatch")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checksumFor(path, asset string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > 4<<20 {
		return "", fmt.Errorf("checksum file is too large")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != asset {
			continue
		}
		hash := strings.ToLower(fields[0])
		if len(hash) != 64 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
			return "", fmt.Errorf("invalid checksum line")
		}
		return hash, nil
	}
	return "", fmt.Errorf("no checksum for %s", asset)
}

func ownerOf(repo config.GitRepo) string {
	owner, _, _ := repo.GitHubOwnerRepo()
	return owner
}

func repoOf(repo config.GitRepo) string {
	_, name, _ := repo.GitHubOwnerRepo()
	return name
}

func ghEnvironment() []string {
	return []string{"GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1"}
}

func copyRegularFile(src, dest string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", filepath.Base(src))
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dest)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func randomSuffix() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UTC().UnixNano())
}

func dryRunGitRef(sel gitSelector) string {
	switch sel.kind {
	case "commit":
		return "commit:" + shortSHA(sel.value, 8)
	case "release":
		return "release:" + sel.value
	case "release-asset":
		return "release:" + sel.value + ":" + sel.asset
	default:
		return "branch:<default>"
	}
}
