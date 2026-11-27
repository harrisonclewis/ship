package config

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// GitRepo is a validated Git repository URL for a local build source.
//
// Transport is the URL exactly as Git should use it. It is never rewritten
// (for example .git is not stripped, and scp-style relative paths stay
// scp-style), because those distinctions are meaningful to Git transports.
//
// The repository URL lives only in the machine-local ship.yaml. It is never
// part of an app's server record, so a server cannot choose a local build
// input or cause this machine to use Git credentials.
type GitRepo struct {
	Scheme    string
	User      string
	Host      string
	RepoPath  string
	Transport string
}

// LocalSource is one servers.<name>.sources.<app> value: either a local
// directory or a Git repository URL.
type LocalSource struct {
	IsGit bool
	Path  string
	Git   GitRepo
}

var (
	scpGitRE  = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9._-]*)@([A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?):(.+)$`)
	sshUserRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
)

// ParseLocalSource validates a source value. Directories stay absolute paths
// (with ~ expanded); Git URLs are checked for a safe, supported form.
func ParseLocalSource(value string) (LocalSource, error) {
	if value == "" {
		return LocalSource{}, fmt.Errorf("source is empty; want a directory or a Git URL")
	}
	if strings.ContainsAny(value, "\n\r\x00") {
		return LocalSource{}, fmt.Errorf("control characters are not allowed")
	}
	if strings.HasPrefix(value, "-") {
		return LocalSource{}, fmt.Errorf("must not start with a dash")
	}
	if repo, ok, err := parseGitSource(value); err != nil {
		return LocalSource{}, err
	} else if ok {
		return LocalSource{IsGit: true, Git: repo}, nil
	}
	if strings.Contains(value, "://") {
		return LocalSource{}, fmt.Errorf("unsupported Git URL scheme (use https:// or ssh://)")
	}

	local, err := expandHome(value)
	if err != nil {
		return LocalSource{}, err
	}
	if !filepath.IsAbs(local) || filepath.Clean(local) != local {
		return LocalSource{}, fmt.Errorf("want a clean absolute path, ~/path or a Git URL")
	}
	return LocalSource{Path: local}, nil
}

// ValidateLocalSourceValue is the validation used by interactive editors
// before a value is written to ship.yaml.
func ValidateLocalSourceValue(value string) error {
	_, err := ParseLocalSource(value)
	return err
}

// NormalizeBuildDir validates a relative build directory inside a Git
// checkout. It is portable across operating systems: it uses forward slashes,
// has no drive or absolute prefix, and never contains a parent component.
// Blank and "." both mean the checkout root and return "".
func NormalizeBuildDir(value string) (string, error) {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.ContainsRune(value, '\\') {
		return "", fmt.Errorf("control characters and backslashes are not allowed")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, ":") {
		return "", fmt.Errorf("want a relative directory without a drive or scheme prefix, got %q", value)
	}
	if path.IsAbs(value) {
		return "", fmt.Errorf("want a directory relative to the Git checkout, not an absolute path: %q", value)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", fmt.Errorf("must not contain a parent directory: %q", value)
		}
	}
	clean := path.Clean(value)
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

// GitSource returns the parsed repository when value is a Git URL.
func GitSource(value string) (GitRepo, bool) {
	loc, err := ParseLocalSource(value)
	if err != nil || !loc.IsGit {
		return GitRepo{}, false
	}
	return loc.Git, true
}

// CacheKey is the host, transport flavour and repository path below the
// per-server cache. The complete identity is used so two repositories with
// the same basename never share a cache directory, and scp-relative and
// ssh-absolute paths to the same host stay separate. Characters outside the
// small portable set are percent-encoded.
func (r GitRepo) CacheKey() string {
	segments := []string{r.Host, r.Scheme}
	if r.User != "" {
		segments = append(segments, r.User)
	}
	segments = append(segments, strings.Split(r.RepoPath, "/")...)
	for i, s := range segments {
		segments[i] = fsSegment(s)
	}
	return path.Join(segments...)
}

// GitHubOwnerRepo returns owner/repo for github.com URLs.
func (r GitRepo) GitHubOwnerRepo() (string, string, bool) {
	if r.Host != "github.com" {
		return "", "", false
	}
	parts := strings.Split(r.RepoPath, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func parseGitSource(value string) (GitRepo, bool, error) {
	if m := scpGitRE.FindStringSubmatch(value); m != nil {
		repoPath, err := cleanGitPath(m[3])
		if err != nil {
			return GitRepo{}, true, err
		}
		if !sshUserRE.MatchString(m[1]) || strings.HasPrefix(m[1], "-") {
			return GitRepo{}, true, fmt.Errorf("invalid SSH user in Git URL")
		}
		return GitRepo{
			Scheme:    "scp",
			User:      m[1],
			Host:      strings.ToLower(m[2]),
			RepoPath:  repoPath,
			Transport: value,
		}, true, nil
	}

	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "ssh://") {
		return GitRepo{}, false, nil
	}

	u, err := url.Parse(value)
	if err != nil {
		return GitRepo{}, true, fmt.Errorf("invalid Git URL; check the scheme, host and path")
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "https", "ssh":
	default:
		return GitRepo{}, true, fmt.Errorf("unsupported Git URL scheme %q (use https:// or ssh://)", scheme)
	}
	if u.Opaque != "" || u.Host == "" {
		return GitRepo{}, true, fmt.Errorf("invalid Git URL; check the host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return GitRepo{}, true, fmt.Errorf("Git URLs must not have a query or fragment")
	}
	host := strings.ToLower(u.Host)
	if err := validateGitHost(strings.ToLower(u.Hostname())); err != nil {
		return GitRepo{}, true, err
	}
	if strings.ContainsAny(host, `/\`) || strings.ContainsAny(host, "\n\r\x00") || strings.HasPrefix(host, "-") {
		return GitRepo{}, true, fmt.Errorf("invalid Git URL host")
	}
	user := ""
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return GitRepo{}, true, fmt.Errorf("Git URLs must not contain credentials")
		}
		user = u.User.Username()
		if scheme == "http" || scheme == "https" {
			if user != "" {
				return GitRepo{}, true, fmt.Errorf("Git URLs must not contain credentials")
			}
		} else if user != "" && (!sshUserRE.MatchString(user) || strings.HasPrefix(user, "-")) {
			return GitRepo{}, true, fmt.Errorf("invalid SSH user in Git URL")
		}
	}
	repoPath, err := cleanGitPath(u.Path)
	if err != nil {
		return GitRepo{}, true, err
	}
	// Git treats the path in ssh:// as absolute, unlike scp-style paths.
	// "ssh" and "scp" are therefore kept as distinct cache flavours.
	return GitRepo{
		Scheme:    scheme,
		User:      user,
		Host:      host,
		RepoPath:  repoPath,
		Transport: value,
	}, true, nil
}

func validateGitHost(host string) error {
	if host == "" || host == "." || host == ".." {
		return fmt.Errorf("invalid Git URL host")
	}
	if strings.ContainsAny(host, `/\`) || strings.ContainsAny(host, "\n\r\x00") || strings.HasPrefix(host, "-") {
		return fmt.Errorf("invalid Git URL host")
	}
	// A host segment must survive CacheKey without becoming a parent
	// directory or a separate path component.
	if strings.Contains(host, "..") && !strings.Contains(host, ":") {
		// DNS names may contain consecutive dots only in malformed input.
		return fmt.Errorf("invalid Git URL host")
	}
	return nil
}

func cleanGitPath(value string) (string, error) {
	p := strings.TrimPrefix(value, "/")
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimSuffix(p, ".git")
	if p == "" {
		return "", fmt.Errorf("Git URL has no repository path")
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid Git URL path")
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
				return "", fmt.Errorf("unsupported character in Git URL path")
			}
		}
	}
	return strings.Join(parts, "/"), nil
}

func fsSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}
