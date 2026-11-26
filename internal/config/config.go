// Package config loads and validates ship.yaml.
//
// Validation is deliberately strict: every name, domain, tag, path and
// environment value that can reach a shell line on the VPS is checked here
// first. The server-side agent validates the same things again.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	DefaultArch       = "linux/amd64"
	DefaultCaddyImage = "caddy:2.10-alpine"
	DefaultAdminUser  = "ops"
	DefaultBackupDir  = "~/backups/vps"

	// ConfigName is the file ship looks for.
	ConfigName = "ship.yaml"
)

var (
	// ReleaseTagRE is the legacy lewbox release-tag pattern.
	ReleaseTagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)
	// ImageVersionRE accepts git describe output, including -dirty-<ts>.
	ImageVersionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	appNameRE      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	adminUserRE    = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	userHostRE     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*@[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	repoRE         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	upstreamRE     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]:[0-9]{1,5}$`)
	serviceRE      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	envNameRE      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	envValueRE     = regexp.MustCompile(`^[A-Za-z0-9_./:@,+*%~=?-]*$`)
	imageNameRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
	imageTagRE     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	serverPathRE   = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	sha256RE       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	pgIdentifierRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
)

// reservedServerNames are command words that must not also name a server.
var reservedServerNames = map[string]bool{
	"setup":       true,
	"lock-ssh":    true,
	"vps":         true,
	"proxy":       true,
	"deploy":      true,
	"rollback":    true,
	"status":      true,
	"logs":        true,
	"restart":     true,
	"maintenance": true,
	"env":         true,
	"backup":      true,
	"backups":     true,
	"restore":     true,
	"version":     true,
	"update":      true,
	"help":        true,
	"all":         true,
}

// Config is the contents of ship.yaml.
type Config struct {
	BackupDirLocal string             `yaml:"backup_dir_local"`
	Servers        map[string]*Server `yaml:"servers"`

	// The flat pre-servers format is only decoded so Validate can point at
	// the migration instead of failing with an unknown-field error.
	Host       string          `yaml:"host"`
	AdminUser  string          `yaml:"admin_user"`
	Arch       string          `yaml:"arch"`
	CaddyImage string          `yaml:"caddy_image"`
	Apps       map[string]*App `yaml:"apps"`

	// Dir is the absolute directory containing ship.yaml. App files are read
	// relative to it, not to the current directory. It is not part of the file.
	Dir string `yaml:"-"`
}

// Server is one VPS and the apps deployed on it.
type Server struct {
	Name string `yaml:"-"`
	Host string `yaml:"host"`
	// IdentityFile is an optional SSH private key for this server. ~/ is
	// expanded; Validate stores the expanded path.
	IdentityFile string          `yaml:"identity_file"`
	AdminUser    string          `yaml:"admin_user"`
	Arch         string          `yaml:"arch"`
	CaddyImage   string          `yaml:"caddy_image"`
	Apps         map[string]*App `yaml:"apps"`

	Dir string `yaml:"-"`
}

// App is one deployed application.
type App struct {
	Domains   []string          `yaml:"domains"`
	Redirects []string          `yaml:"redirects"`
	Upstream  string            `yaml:"upstream"`
	Source    Source            `yaml:"source"`
	UID       *int              `yaml:"uid"`
	Data      []string          `yaml:"data"`
	Health    Health            `yaml:"health"`
	Backup    BackupSpec        `yaml:"backup"`
	Env       map[string]string `yaml:"env"`
	Build     BuildSpec         `yaml:"build"`
	Deploy    DeploySpec        `yaml:"deploy"`
}

// BuildSpec is a local script run before an image app is built.
type BuildSpec struct {
	Commands string `yaml:"commands"`
}

// DeploySpec is a server-side script run in the new image before a switch,
// and the maintenance-page setting used during the deploy.
type DeploySpec struct {
	Commands    string `yaml:"commands"`
	Maintenance *bool  `yaml:"maintenance"`
}

// MaintenanceEnabled reports whether deploys should show the maintenance page
// while switching. It defaults to true when the setting is omitted.
func (a *App) MaintenanceEnabled() bool {
	return a.Deploy.Maintenance == nil || *a.Deploy.Maintenance
}

// Source describes where an app's image comes from.
type Source struct {
	Type  string `yaml:"type"`
	Repo  string `yaml:"repo"`
	Asset string `yaml:"asset"`
	Path  string `yaml:"path"`
}

// Health is an optional extra health check performed from inside the web
// network, in addition to the compose healthcheck.
type Health struct {
	HTTPPath string `yaml:"http_path"`
}

// BackupSpec says what to archive for an app.
type BackupSpec struct {
	SQLite   []string      `yaml:"sqlite"`
	Files    []string      `yaml:"files"`
	Postgres *PostgresSpec `yaml:"postgres"`
}

// PostgresSpec dumps a database with pg_dump inside a compose service.
type PostgresSpec struct {
	Service string `yaml:"service"`
	DB      string `yaml:"db"`
	User    string `yaml:"user"`
}

// Load reads and validates ship.yaml and records its directory.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c, err := Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.setDir(filepath.Dir(abs))
	return c, nil
}

func (c *Config) setDir(dir string) {
	c.Dir = dir
	for _, s := range c.Servers {
		s.Dir = dir
	}
}

// UserConfigPath returns <user config dir>/ship/ship.yaml.
//
// Linux uses os.UserConfigDir (XDG_CONFIG_HOME, else ~/.config). macOS uses
// ~/.config to match lewdev, and Windows uses %USERPROFILE%\.config.
func UserConfigPath() (string, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "ship", ConfigName), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ship", ConfigName), nil
}

// Find resolves ship.yaml: $SHIP_CONFIG, then ./ship.yaml, then the user
// config dir. SHIP_CONFIG is authoritative: if set, only that path is used.
func Find() (string, error) {
	if p := os.Getenv("SHIP_CONFIG"); p != "" {
		fi, err := os.Stat(p)
		if err != nil {
			return "", fmt.Errorf("SHIP_CONFIG: %w", err)
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("SHIP_CONFIG: %s is not a regular file", p)
		}
		return p, nil
	}

	looked := []string{ConfigName}
	if p, err := UserConfigPath(); err == nil {
		looked = append(looked, p)
	}
	for _, p := range looked {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found; looked in: %s", ConfigName, strings.Join(looked, ", "))
}

// LoadDefault finds and loads ship.yaml.
func LoadDefault() (*Config, error) {
	path, err := Find()
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// Decode parses and validates a ship.yaml document.
func Decode(r io.Reader) (*Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			// An empty or comment-only file is a valid empty configuration.
			if err := c.Validate(); err != nil {
				return nil, err
			}
			return &c, nil
		}
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("ship.yaml must contain exactly one YAML document")
		}
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks every field and applies defaults.
func (c *Config) Validate() error {
	if c.Host != "" || c.AdminUser != "" || c.Arch != "" || c.CaddyImage != "" || len(c.Apps) > 0 {
		return errors.New("the flat host/apps format is no longer supported; move host, admin_user, arch, caddy_image and apps under servers.<name>")
	}

	if c.BackupDirLocal == "" {
		c.BackupDirLocal = DefaultBackupDir
	}
	dir, err := expandHome(c.BackupDirLocal)
	if err != nil {
		return fmt.Errorf("backup_dir_local: %w", err)
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("backup_dir_local: %q is not absolute after expanding ~", dir)
	}
	if strings.ContainsAny(c.BackupDirLocal, "\n\r\x00") {
		return errors.New("backup_dir_local: control characters are not allowed")
	}
	c.BackupDirLocal = dir

	seenHost := map[string]string{}
	for _, name := range c.ServerNames() {
		srv := c.Servers[name]
		if err := validateServerName(name); err != nil {
			return err
		}
		if srv == nil {
			return fmt.Errorf("servers.%s: server definition is empty", name)
		}
		srv.Name = name
		srv.Dir = c.Dir
		if err := srv.validate(seenHost); err != nil {
			return err
		}
	}
	return nil
}

func validateServerName(name string) error {
	if reservedServerNames[name] {
		return fmt.Errorf("servers.%s: %q is reserved", name, name)
	}
	if !appNameRE.MatchString(name) {
		return fmt.Errorf("servers.%s: invalid server name (want lowercase letters, digits and dashes, 2-32 chars)", name)
	}
	return nil
}

func (s *Server) validate(seenHost map[string]string) error {
	p := "servers." + s.Name
	if !userHostRE.MatchString(s.Host) {
		return fmt.Errorf("%s.host: want user@host (for example ops@203.0.113.10), got %q", p, s.Host)
	}
	if s.AdminUser == "" {
		s.AdminUser = DefaultAdminUser
	}
	if s.IdentityFile != "" {
		key, err := expandHome(s.IdentityFile)
		if err != nil {
			return fmt.Errorf("%s.identity_file: %w", p, err)
		}
		if !filepath.IsAbs(key) || filepath.Clean(key) != key || strings.ContainsAny(key, "\n\r\x00") {
			return fmt.Errorf("%s.identity_file: want an absolute path or ~/path, got %q", p, s.IdentityFile)
		}
		s.IdentityFile = key
	}
	if !adminUserRE.MatchString(s.AdminUser) || s.AdminUser == "deploy" || s.AdminUser == "root" {
		return fmt.Errorf("%s.admin_user: invalid user %q (root and deploy are reserved)", p, s.AdminUser)
	}
	if s.Arch == "" {
		s.Arch = DefaultArch
	}
	switch s.Arch {
	case "linux/amd64", "amd64":
		s.Arch = "linux/amd64"
	case "linux/arm64", "arm64":
		s.Arch = "linux/arm64"
	default:
		return fmt.Errorf("%s.arch: want linux/amd64 or linux/arm64, got %q", p, s.Arch)
	}
	if s.CaddyImage == "" {
		s.CaddyImage = DefaultCaddyImage
	}
	if err := ValidateImageRef(s.CaddyImage); err != nil {
		return fmt.Errorf("%s.caddy_image: %w", p, err)
	}

	for _, name := range s.AppNames() {
		if err := checkAppName(p+".apps", name); err != nil {
			return err
		}
		app := s.Apps[name]
		if app == nil {
			return fmt.Errorf("%s.apps.%s: app definition is empty", p, name)
		}
		if err := s.validateApp(p+".apps."+name, app, seenHost); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) validateApp(p string, app *App, seenHost map[string]string) error {
	if len(app.Domains) == 0 {
		return fmt.Errorf("%s.domains: at least one domain is required", p)
	}
	for i, d := range app.Domains {
		if err := ValidateDomain(d); err != nil {
			return fmt.Errorf("%s.domains[%d]: %w", p, i, err)
		}
		if prev, ok := seenHost[d]; ok {
			return fmt.Errorf("%s.domains[%d]: %q is already used by %s", p, i, d, prev)
		}
		seenHost[d] = p
	}
	for i, d := range app.Redirects {
		if err := ValidateDomain(d); err != nil {
			return fmt.Errorf("%s.redirects[%d]: %w", p, i, err)
		}
		if prev, ok := seenHost[d]; ok {
			return fmt.Errorf("%s.redirects[%d]: %q is already used by %s", p, i, d, prev)
		}
		seenHost[d] = p
	}

	if !upstreamRE.MatchString(app.Upstream) {
		return fmt.Errorf("%s.upstream: want service:port (for example lewbox:8080), got %q", p, app.Upstream)
	}
	if port, _ := strconv.Atoi(app.Upstream[strings.LastIndex(app.Upstream, ":")+1:]); port < 1 || port > 65535 {
		return fmt.Errorf("%s.upstream: port out of range in %q", p, app.Upstream)
	}

	switch app.Source.Type {
	case "github-release":
		if !repoRE.MatchString(app.Source.Repo) {
			return fmt.Errorf("%s.source.repo: want owner/repo, got %q", p, app.Source.Repo)
		}
		if app.Source.Asset == "" {
			app.Source.Asset = "{app}_linux_{arch}"
		}
		if !strings.Contains(app.Source.Asset, "{arch}") {
			return fmt.Errorf("%s.source.asset: must contain {arch}", p)
		}
		if !assetPatternOK(app.Source.Asset) {
			return fmt.Errorf("%s.source.asset: invalid asset pattern %q", p, app.Source.Asset)
		}
		if app.Source.Path != "" {
			return fmt.Errorf("%s.source.path: only valid for source.type image", p)
		}
	case "image":
		if app.Source.Path == "" {
			return fmt.Errorf("%s.source.path: required for source.type image", p)
		}
		local, err := expandHome(app.Source.Path)
		if err != nil {
			return fmt.Errorf("%s.source.path: %w", p, err)
		}
		if !filepath.IsAbs(local) || filepath.Clean(local) != local {
			return fmt.Errorf("%s.source.path: want a clean absolute path or ~/path, got %q", p, app.Source.Path)
		}
		app.Source.Path = local
		if strings.ContainsAny(app.Source.Path, "\n\r\x00") {
			return fmt.Errorf("%s.source.path: control characters are not allowed", p)
		}
		if app.Source.Repo != "" || app.Source.Asset != "" {
			return fmt.Errorf("%s.source: repo/asset are only valid for source.type github-release", p)
		}
	default:
		return fmt.Errorf("%s.source.type: want github-release or image, got %q", p, app.Source.Type)
	}

	if err := normalizeScript(p+".build.commands", &app.Build.Commands); err != nil {
		return err
	}
	if err := normalizeScript(p+".deploy.commands", &app.Deploy.Commands); err != nil {
		return err
	}
	if strings.TrimSpace(app.Build.Commands) != "" && app.Source.Type != "image" {
		return fmt.Errorf("%s.build.commands: only valid for source.type image", p)
	}

	if app.UID != nil {
		if *app.UID < 1 || *app.UID > 2147483647 {
			return fmt.Errorf("%s.uid: out of range: %d", p, *app.UID)
		}
	}
	for i, d := range app.Data {
		if err := validateServerPath(d); err != nil {
			return fmt.Errorf("%s.data[%d]: %w", p, i, err)
		}
	}
	if app.Health.HTTPPath != "" {
		if !strings.HasPrefix(app.Health.HTTPPath, "/") || strings.ContainsAny(app.Health.HTTPPath, " \t\n\r\x00\"'\\") {
			return fmt.Errorf("%s.health.http_path: want a path starting with / and no spaces or quotes, got %q", p, app.Health.HTTPPath)
		}
	}
	for i, d := range app.Backup.SQLite {
		if err := validateServerPath(d); err != nil {
			return fmt.Errorf("%s.backup.sqlite[%d]: %w", p, i, err)
		}
		if app.UID == nil {
			return fmt.Errorf("%s.backup.sqlite: uid is required so sqlite runs as the app user", p)
		}
	}
	for i, d := range app.Backup.Files {
		if err := validateServerPath(d); err != nil {
			return fmt.Errorf("%s.backup.files[%d]: %w", p, i, err)
		}
	}
	if pg := app.Backup.Postgres; pg != nil {
		if !serviceRE.MatchString(pg.Service) {
			return fmt.Errorf("%s.backup.postgres.service: invalid service %q", p, pg.Service)
		}
		if !pgIdentifierRE.MatchString(pg.DB) {
			return fmt.Errorf("%s.backup.postgres.db: invalid database name %q", p, pg.DB)
		}
		if !pgIdentifierRE.MatchString(pg.User) {
			return fmt.Errorf("%s.backup.postgres.user: invalid user %q", p, pg.User)
		}
	}
	for k, v := range app.Env {
		if k == "IMAGE" {
			return fmt.Errorf("%s.env: IMAGE is reserved for ship", p)
		}
		if !envNameRE.MatchString(k) {
			return fmt.Errorf("%s.env: invalid variable name %q", p, k)
		}
		if !envValueRE.MatchString(v) || len(v) > 4096 {
			return fmt.Errorf("%s.env.%s: value must use only letters, digits and _./:@,+*%%~=?- (no spaces, quotes, $ or #)", p, k)
		}
	}
	return nil
}

// checkAppName rejects names that could be confused with agent actions or
// that docker compose would not accept as a project/service name.
func checkAppName(prefix, name string) error {
	if name == "all" || name == "proxy" {
		return fmt.Errorf("%s.%s: %q is reserved", prefix, name, name)
	}
	if !appNameRE.MatchString(name) {
		return fmt.Errorf("%s.%s: invalid app name (want lowercase letters, digits and dashes, 2-32 chars)", prefix, name)
	}
	return nil
}

func assetPatternOK(s string) bool {
	rest := strings.ReplaceAll(strings.ReplaceAll(s, "{arch}", ""), "{app}", "")
	if rest == "" || strings.ContainsAny(rest, "{}[]/\\ \t\n\r\x00") {
		return false
	}
	for _, r := range rest {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// ValidateDomain accepts a lowercase FQDN without a trailing dot.
func ValidateDomain(d string) error {
	if len(d) == 0 || len(d) > 253 {
		return fmt.Errorf("invalid domain length: %q", d)
	}
	if strings.ToLower(d) != d {
		return fmt.Errorf("domain must be lowercase: %q", d)
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must contain a dot: %q", d)
	}
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 {
			return fmt.Errorf("invalid DNS label in %q", d)
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return fmt.Errorf("DNS label starts or ends with a dash in %q", d)
		}
		for _, r := range l {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("invalid character in domain %q", d)
			}
		}
	}
	return nil
}

// normalizeScript validates a multi-line shell script and converts CRLF line
// endings to LF so a Windows editor does not silently break it.
func normalizeScript(field string, script *string) error {
	const maxScript = 256 << 10
	*script = strings.ReplaceAll(*script, "\r\n", "\n")
	if strings.ContainsAny(*script, "\x00\r") {
		return fmt.Errorf("%s: use LF line endings and no NUL characters", field)
	}
	if len(*script) > maxScript {
		return fmt.Errorf("%s: script is larger than %d bytes", field, maxScript)
	}
	return nil
}

func validateServerPath(p string) error {
	if p == "" || !serverPathRE.MatchString(p) {
		return fmt.Errorf("path must be absolute and use only letters, digits and ._/-: %q", p)
	}
	// Server paths are POSIX paths, whatever OS ship runs on.
	if !path.IsAbs(p) || path.Clean(p) != p || p == "/" {
		return fmt.Errorf("path must be a clean absolute path other than /: %q", p)
	}
	return nil
}

// ValidateImageRef checks a docker image reference with an explicit tag or
// digest (pins are required for infrastructure images).
func ValidateImageRef(ref string) error {
	if ref == "" || strings.ContainsAny(ref, " \t\n\r\x00\"'\\$`;|&<>()") {
		return fmt.Errorf("invalid image reference %q", ref)
	}
	base := ref
	if i := strings.Index(base, "@"); i >= 0 {
		digest := base[i+1:]
		base = base[:i]
		if !strings.HasPrefix(digest, "sha256:") || !sha256RE.MatchString(strings.TrimPrefix(digest, "sha256:")) {
			return fmt.Errorf("invalid digest in %q", ref)
		}
	}
	tag := ""
	if i := strings.LastIndex(base, ":"); i > strings.LastIndex(base, "/") {
		tag = base[i+1:]
		base = base[:i]
	}
	if tag == "" && !strings.Contains(ref, "@") {
		return fmt.Errorf("image %q must pin a tag or sha256 digest", ref)
	}
	if tag != "" && !imageTagRE.MatchString(tag) {
		return fmt.Errorf("invalid image tag in %q", ref)
	}
	if !imageNameRE.MatchString(base) {
		return fmt.Errorf("invalid image name in %q", ref)
	}
	return nil
}

// ValidateReleaseTag checks a published release tag with the legacy regex.
func ValidateReleaseTag(tag string) error {
	if !ReleaseTagRE.MatchString(tag) {
		return fmt.Errorf("not a release tag: %q", tag)
	}
	return nil
}

// ValidateImageVersion checks a locally built image tag.
func ValidateImageVersion(v string) error {
	if !ImageVersionRE.MatchString(v) {
		return fmt.Errorf("invalid image version %q", v)
	}
	return nil
}

// ValidateAppName is the exported app-name check used by the CLI before it
// loads the config for path construction.
func ValidateAppName(name string) error { return checkAppName("apps", name) }

// ValidateService checks a compose service name.
func ValidateService(s string) error {
	if !serviceRE.MatchString(s) {
		return fmt.Errorf("invalid service name %q", s)
	}
	return nil
}

// ValidateLabel checks a backup label.
func ValidateLabel(label string) error {
	if label == "" || len(label) > 80 {
		return fmt.Errorf("invalid backup label %q", label)
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return fmt.Errorf("invalid backup label %q", label)
		}
	}
	return nil
}

// ServerNames returns server names sorted for deterministic output.
func (c *Config) ServerNames() []string {
	names := make([]string, 0, len(c.Servers))
	for n := range c.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Server returns a configured server or an error suitable for a CLI.
func (c *Config) Server(name string) (*Server, error) {
	s, ok := c.Servers[name]
	if !ok {
		return nil, fmt.Errorf("unknown server %q (configured: %s)", name, strings.Join(c.ServerNames(), ", "))
	}
	return s, nil
}

// AppNames returns app names sorted for deterministic output.
func (s *Server) AppNames() []string {
	names := make([]string, 0, len(s.Apps))
	for n := range s.Apps {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// App returns a configured app or an error suitable for a CLI.
func (s *Server) App(name string) (*App, error) {
	app, ok := s.Apps[name]
	if !ok {
		return nil, fmt.Errorf("unknown app %q on server %s (configured: %s)", name, s.Name, strings.Join(s.AppNames(), ", "))
	}
	return app, nil
}

// PlatformArch returns amd64 or arm64 for release assets.
func (s *Server) PlatformArch() string {
	return strings.TrimPrefix(s.Arch, "linux/")
}

// ImageRepo returns the local docker image repository ship uses for an app.
// Image apps are built locally and streamed to the VPS; release apps are built
// on the server and keep the app name as their repository.
func (s *Server) ImageRepo(name string) string {
	app := s.Apps[name]
	if app != nil && app.Source.Type == "image" {
		return "ship/" + name
	}
	return name
}

// ImageRef returns the full image reference for an app version.
func (s *Server) ImageRef(name, version string) string {
	return s.ImageRepo(name) + ":" + version
}

// ImageApps returns names of apps built locally.
func (s *Server) ImageApps() []string {
	var out []string
	for _, n := range s.AppNames() {
		if s.Apps[n].Source.Type == "image" {
			out = append(out, n)
		}
	}
	return out
}

// AppsSpec is the app:uid list passed to ship-agent setup.
func (s *Server) AppsSpec() string {
	var parts []string
	for _, n := range s.AppNames() {
		if uid := s.Apps[n].UID; uid != nil {
			parts = append(parts, n+":"+strconv.Itoa(*uid))
		}
	}
	return strings.Join(parts, ",")
}

// ResolveAsset expands {arch} and {app} in a release asset pattern.
func (s *Server) ResolveAsset(name string) string {
	a := s.Apps[name].Source.Asset
	a = strings.ReplaceAll(a, "{arch}", s.PlatformArch())
	a = strings.ReplaceAll(a, "{app}", name)
	return a
}

// Meta renders the server-side descriptor for an app. It is a flat
// key=value file so the bash agent can read it without eval.
func (s *Server) Meta(name string) string {
	app := s.Apps[name]
	upstreamService := app.Upstream[:strings.LastIndex(app.Upstream, ":")]
	var b strings.Builder
	line := func(k, v string) { fmt.Fprintf(&b, "%s=%s\n", k, v) }
	line("version", "1")
	line("app", name)
	line("server", s.Name)
	line("source", app.Source.Type)
	line("repo", app.Source.Repo)
	line("asset", s.ResolveAsset(name))
	line("image_repo", s.ImageRepo(name))
	line("service", upstreamService)
	line("upstream", app.Upstream)
	line("canonical", app.Domains[0])
	line("domains", strings.Join(app.Domains, " "))
	line("http_path", app.Health.HTTPPath)
	if app.MaintenanceEnabled() {
		line("maintenance", "on")
	} else {
		line("maintenance", "off")
	}
	if app.UID != nil {
		line("uid", strconv.Itoa(*app.UID))
	} else {
		line("uid", "")
	}
	line("data", strings.Join(app.Data, " "))
	line("sqlite", strings.Join(app.Backup.SQLite, " "))
	line("files", strings.Join(app.Backup.Files, " "))
	pgService, pgDB, pgUser := "", "", ""
	if pg := app.Backup.Postgres; pg != nil {
		pgService, pgDB, pgUser = pg.Service, pg.DB, pg.User
	}
	line("postgres_service", pgService)
	line("postgres_db", pgDB)
	line("postgres_user", pgUser)
	return b.String()
}

// ShipEnv renders non-secret app environment values for .ship-env. The agent
// prepends IMAGE when it writes .ship.env.
func (s *Server) ShipEnv(name string) string {
	app := s.Apps[name]
	keys := make([]string, 0, len(app.Env))
	for k := range app.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, app.Env[k])
	}
	return b.String()
}

// ComposePath is the compose file for an app, next to ship.yaml.
func (s *Server) ComposePath(name string) string {
	return filepath.Join(s.Dir, "apps", name, "compose.yaml")
}

// DockerfilePath is the Dockerfile for a release app, next to ship.yaml.
func (s *Server) DockerfilePath(name string) string {
	return filepath.Join(s.Dir, "apps", name, "Dockerfile")
}

func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	if strings.HasPrefix(p, "~") {
		return "", fmt.Errorf("only ~/... expansion is supported, got %q", p)
	}
	return p, nil
}
