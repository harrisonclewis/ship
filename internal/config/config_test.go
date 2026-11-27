package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const validYAML = `backup_dir_local: ~/ship-backups
servers:
  prod:
    host: ops@203.0.113.10
    sources:
      commerce: ~/commerce
`

func decode(t *testing.T, doc string) (*Config, error) {
	t.Helper()
	return Decode(strings.NewReader(doc))
}

func server(t *testing.T, c *Config, name string) *Server {
	t.Helper()
	s, err := c.Server(name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDecodeValid(t *testing.T) {
	c, err := decode(t, validYAML)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := c.ServerNames(); strings.Join(got, ",") != "prod" {
		t.Fatalf("ServerNames = %v", got)
	}
	srv := server(t, c, "prod")
	home, _ := os.UserHomeDir()
	if got := srv.Sources["commerce"]; got != filepath.Join(home, "commerce") {
		t.Fatalf("sources.commerce = %q", got)
	}
	if got := c.BackupDirLocal; got != filepath.Join(home, "ship-backups") {
		t.Fatalf("BackupDirLocal = %q", got)
	}
}

func TestDecodeUnknownField(t *testing.T) {
	if _, err := decode(t, validYAML+"bogus: 1\n"); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestOldFullFormatExplainsMigration(t *testing.T) {
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    admin_user: ops
    arch: linux/amd64
    caddy_image: caddy:2.10-alpine
    apps:
      lewbox:
        domains: [cloud.example.com]
        upstream: lewbox:8080
        source:
          type: github-release
          repo: harlew-dev/lewbox
`
	_, err := decode(t, doc)
	if err == nil {
		t.Fatal("old format accepted")
	}
	if !strings.Contains(err.Error(), "config push") {
		t.Fatalf("error does not explain the migration: %v", err)
	}
}

func TestOldFlatFormatExplainsMigration(t *testing.T) {
	doc := `host: ops@203.0.113.10
apps:
  lewbox:
    domains: [cloud.example.com]
    upstream: lewbox:8080
    source:
      type: github-release
      repo: harlew-dev/lewbox
`
	_, err := decode(t, doc)
	if err == nil {
		t.Fatal("old flat format accepted")
	}
	if !strings.Contains(err.Error(), "config push") {
		t.Fatalf("error does not explain the migration: %v", err)
	}
}

func TestLoadOldAcceptsFullFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    admin_user: ops
    arch: linux/amd64
    caddy_image: caddy:2.10-alpine
    apps:
      lewbox:
        domains: [cloud.example.com]
        upstream: lewbox:8080
        source:
          type: github-release
          repo: harlew-dev/lewbox
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := c.Servers["prod"]
	if srv.AdminUser != "ops" || srv.Arch != "linux/amd64" || srv.Apps["lewbox"] == nil {
		t.Fatalf("old config not loaded: %+v", srv)
	}
}

func TestDefaults(t *testing.T) {
	doc := `servers:
  prod:
    host: root@203.0.113.10
`
	c, err := decode(t, doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	srv := server(t, c, "prod")
	if srv.AdminUser != "" || srv.CaddyImage != "" || srv.Arch != "" {
		t.Fatalf("local file must not hold server settings: %+v", srv)
	}
	if !strings.HasSuffix(c.BackupDirLocal, filepath.Join("backups", "vps")) {
		t.Fatalf("default local backup dir = %q", c.BackupDirLocal)
	}
}

func TestRejectInvalidLocalConfig(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"bad host", strings.Replace(validYAML, "ops@203.0.113.10", "ops 203.0.113.10", 1), "host"},
		{"relative identity", strings.Replace(validYAML, "    host: ops@203.0.113.10\n", "    host: ops@203.0.113.10\n    identity_file: keys/prod\n", 1), "identity_file"},
		{"relative source", strings.Replace(validYAML, "~/commerce", "commerce", 1), "absolute"},
		{"bad source app", strings.Replace(validYAML, "      commerce:", "      Commerce:", 1), "sources"},
		{"duplicate domain field", validYAML + "    admin_user: ops\n", "config push"},
		{"empty old apps", validYAML + "    apps: {}\n", "config push"},
		{"empty old admin", validYAML + "    admin_user: \"\"\n", "config push"},
		{"unpinned caddy", validYAML + "    caddy_image: caddy\n", "config push"},
		{"relative backup dir", strings.Replace(validYAML, "backup_dir_local: ~/ship-backups", "backup_dir_local: backups", 1), "absolute"},
		{"removed registry owner", validYAML + "registry_owner: harlew-dev\n", "registry_owner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decode(t, tc.doc)
			if err == nil {
				t.Fatalf("accepted invalid config")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestEmptyConfigAndServersWithoutApps(t *testing.T) {
	if _, err := decode(t, "backup_dir_local: ~/b\nservers: {}\n"); err != nil {
		t.Fatalf("config with no servers rejected: %v", err)
	}
	if _, err := decode(t, "backup_dir_local: ~/b\n"); err != nil {
		t.Fatalf("empty config rejected: %v", err)
	}
	if _, err := decode(t, validYAML); err != nil {
		t.Fatalf("server without apps rejected: %v", err)
	}
}

func TestServerNamesAreReservedCommandWords(t *testing.T) {
	for _, name := range []string{"setup", "deploy", "status", "version", "update", "help", "config", "app", "all", "proxy", "server"} {
		t.Run(name, func(t *testing.T) {
			doc := strings.Replace(validYAML, "  prod:", "  "+name+":", 1)
			_, err := decode(t, doc)
			if err == nil {
				t.Fatalf("server name %q accepted", name)
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("error = %v, want reserved", err)
			}
		})
	}
}

func TestConfigServerNameErrorMentionsConfigCommand(t *testing.T) {
	doc := strings.Replace(validYAML, "  prod:", "  config:", 1)
	_, err := decode(t, doc)
	if err == nil || !strings.Contains(err.Error(), "ship config edit") {
		t.Fatalf("error = %v, want the config command named", err)
	}
}

func TestServerNameRules(t *testing.T) {
	for _, name := range []string{"Prod", "p", "1prod", "prod_server"} {
		doc := strings.Replace(validYAML, "  prod:", "  "+name+":", 1)
		if _, err := decode(t, doc); err == nil {
			t.Errorf("server name %q accepted", name)
		}
	}
}

const validRecord = `domains: [cloud.example.com]
upstream: lewbox:8080
source:
  type: github-release
  repo: harlew-dev/lewbox
uid: 10001
data: [/srv/lewbox]
health:
  http_path: /v0/health
backup:
  sqlite: [/srv/lewbox/lewbox.db]
  files: [/srv/lewbox/keyring.key]
env:
  LEWBOX_TRUSTED_PROXIES: 172.30.0.0/24
`

func record(t *testing.T, doc string) *App {
	t.Helper()
	a, err := DecodeAppRecord(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestValidateRecordsUniquenessAndFields(t *testing.T) {
	a := record(t, validRecord)
	b := record(t, `domains: [other.example.com]
upstream: other:8080
source: {type: github-release, repo: example/other}
`)
	if err := ValidateRecords(map[string]*App{"aa": a, "bb": b}); err != nil {
		t.Fatalf("valid records rejected: %v", err)
	}

	dup := record(t, `domains: [cloud.example.com]
upstream: other:8080
source: {type: github-release, repo: example/other}
`)
	if err := ValidateRecords(map[string]*App{"aa": a, "bb": dup}); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("duplicate domain: %v", err)
	}

	for _, tc := range []struct {
		name string
		key  string
	}{
		{"uppercase app", "Bad"},
		{"reserved app", "all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRecords(map[string]*App{tc.key: a})
			if err == nil {
				t.Fatal("accepted invalid app name")
			}
		})
	}

	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"uppercase domain", strings.Replace(validRecord, "cloud.example.com", "Cloud.example.com", 1), "lowercase"},
		{"domain underscore", strings.Replace(validRecord, "cloud.example.com", "cloud_example.com", 1), "domain"},
		{"bad upstream", strings.Replace(validRecord, "lewbox:8080", "lewbox", 1), "upstream"},
		{"bad upstream port", strings.Replace(validRecord, "lewbox:8080", "lewbox:70000", 1), "port"},
		{"bad repo", strings.Replace(validRecord, "repo: harlew-dev/lewbox", "repo: lewbox", 1), "repo"},
		{"sqlite without uid", strings.Replace(validRecord, "uid: 10001\n", "", 1), "uid is required"},
		{"path traversal", strings.Replace(validRecord, "data: [/srv/lewbox]", "data: [/srv/../etc]", 1), "clean absolute"},
		{"env dollar", strings.Replace(validRecord, "172.30.0.0/24", "$SECRET", 1), "env."},
		{"reserved env", strings.Replace(validRecord, "LEWBOX_TRUSTED_PROXIES:", "IMAGE:", 1), "reserved"},
		{"bad health path", strings.Replace(validRecord, "http_path: /v0/health", "http_path: v0/health", 1), "http_path"},
		{"local source path", `domains: [app.example.com]
upstream: app:8080
source: {type: image, path: /tmp/app}
`, "source.path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := DecodeAppRecord(strings.NewReader(tc.doc))
			if err == nil {
				err = ValidateRecords(map[string]*App{"aa": rec})
			}
			if err == nil {
				t.Fatal("accepted invalid record")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLocalSourceEditing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetLocalSource(path, "prod", "web", "~/src/web", false); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got := c.Servers["prod"].Sources["web"]; got != filepath.Join(home, "src", "web") {
		t.Fatalf("sources.web = %q", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("missing .bak: %v", err)
	}
	if err := SetLocalSource(path, "prod", "web", "", true); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Servers["prod"].Sources["web"]; ok {
		t.Fatal("source was not removed")
	}
}

func TestVersionValidation(t *testing.T) {
	for _, ok := range []string{"v1.2.3", "v0.0.1-rc.1", "v10.20.30-beta"} {
		if err := ValidateReleaseTag(ok); err != nil {
			t.Errorf("release tag %q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"1.2.3", "v1.2", "v1.2.3;rm", "latest", "v1.2.3\n"} {
		if err := ValidateReleaseTag(bad); err == nil {
			t.Errorf("release tag %q accepted", bad)
		}
	}
	for _, ok := range []string{"v1.2.3", "v1.2.3-4-gabc123", "abc123", "v1.2.3-dirty-20260101T000000Z"} {
		if err := ValidateImageVersion(ok); err != nil {
			t.Errorf("image version %q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", "a b", "a;b", "a\nb"} {
		if err := ValidateImageVersion(bad); err == nil {
			t.Errorf("image version %q accepted", bad)
		}
	}
}

func TestValidateImageRef(t *testing.T) {
	good := []string{"caddy:2.10-alpine", "ghcr.io/harlew-dev/lewbox:v1.2.3", "caddy@sha256:" + strings.Repeat("a", 64)}
	for _, ref := range good {
		if err := ValidateImageRef(ref); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
	bad := []string{"caddy", "Caddy:2", "caddy:2;rm", "caddy@sha256:abc", "caddy:2.10-alpine $(id)"}
	for _, ref := range bad {
		if err := ValidateImageRef(ref); err == nil {
			t.Errorf("%q accepted", ref)
		}
	}
}

func TestNormalizeArch(t *testing.T) {
	for in, want := range map[string]string{"x86_64": "linux/amd64", "aarch64": "linux/arm64", "arm64": "linux/arm64"} {
		if got, err := NormalizeArch(in); err != nil || got != want {
			t.Errorf("NormalizeArch(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeArch("riscv64"); err == nil {
		t.Fatal("riscv64 accepted")
	}
}

func TestLoadSetsDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Dir != dir {
		t.Fatalf("Dir = %q, want %q", c.Dir, dir)
	}
	srv := server(t, c, "prod")
	home, _ := os.UserHomeDir()
	if got := srv.Sources["commerce"]; got != filepath.Join(home, "commerce") {
		t.Fatalf("sources.commerce = %q", got)
	}
}

func TestLoadRelativePathUsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ship.yaml"), []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	c, err := Load("ship.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(c.Dir) {
		t.Fatalf("Dir is not absolute: %q", c.Dir)
	}
}

func TestFindUsesShipConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIP_CONFIG", path)
	got, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("Find = %q, want %q", got, path)
	}
}

func TestFindShipConfigIsAuthoritative(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	t.Setenv("SHIP_CONFIG", missing)
	if _, err := Find(); err == nil || !strings.Contains(err.Error(), "SHIP_CONFIG") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindUsesCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ship.yaml"), []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIP_CONFIG", "")
	t.Chdir(dir)
	got, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if got != "ship.yaml" {
		t.Fatalf("Find = %q", got)
	}
}

func TestFindFallsBackToUserConfigDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SHIP_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	t.Chdir(t.TempDir())

	want, err := UserConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte(validYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if want != filepath.Join(base, "ship", "ship.yaml") {
			t.Fatalf("UserConfigPath = %q", want)
		}
	}
	got, err := Find()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Find = %q, want %q", got, want)
	}
	c, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(c.Dir, "ship") {
		t.Fatalf("Dir = %q", c.Dir)
	}
}

func TestFindNotFoundListsCandidates(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SHIP_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	t.Chdir(t.TempDir())

	_, err := Find()
	if err == nil {
		t.Fatal("Find succeeded without ship.yaml")
	}
	msg := err.Error()
	for _, want := range []string{"ship.yaml", filepath.Join("ship", "ship.yaml")} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestMeta(t *testing.T) {
	app := record(t, validRecord)
	srv := &Server{Name: "prod", Arch: "linux/amd64", Apps: map[string]*App{"lewbox": app}}
	meta := srv.Meta("lewbox")
	for _, want := range []string{
		"version=1\n", "app=lewbox\n", "source=github-release\n",
		"repo=harlew-dev/lewbox\n", "asset=lewbox_linux_amd64\n",
		"image_repo=lewbox\n", "service=lewbox\n", "upstream=lewbox:8080\n",
		"canonical=cloud.example.com\n", "domains=cloud.example.com\n",
		"http_path=/v0/health\n", "uid=10001\n", "data=/srv/lewbox\n",
		"sqlite=/srv/lewbox/lewbox.db\n", "files=/srv/lewbox/keyring.key\n",
		"postgres_service=\n",
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("meta missing %q:\n%s", want, meta)
		}
	}
	env := srv.ShipEnv("lewbox")
	if !strings.Contains(env, "LEWBOX_TRUSTED_PROXIES=172.30.0.0/24\n") {
		t.Errorf("ShipEnv = %q", env)
	}
	if strings.Contains(env, "IMAGE=") {
		t.Errorf("ShipEnv must not set IMAGE: %q", env)
	}
	if got := srv.AppsSpec(); got != "lewbox:10001" {
		t.Fatalf("AppsSpec = %q", got)
	}
	if got := srv.PlatformArch(); got != "amd64" {
		t.Fatalf("PlatformArch = %q", got)
	}
}

func TestIdentityFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	withKey := strings.Replace(validYAML, "    host: ops@203.0.113.10\n", "    host: ops@203.0.113.10\n    identity_file: ~/.ssh/prod\n", 1)
	c, err := Decode(strings.NewReader(withKey))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Servers["prod"].IdentityFile, filepath.Join(home, ".ssh", "prod"); got != want {
		t.Fatalf("identity_file = %q, want %q", got, want)
	}
	relative := strings.Replace(validYAML, "    host: ops@203.0.113.10\n", "    host: ops@203.0.113.10\n    identity_file: keys/prod\n", 1)
	if _, err := Decode(strings.NewReader(relative)); err == nil || !strings.Contains(err.Error(), "identity_file") {
		t.Fatalf("relative identity_file: want an identity_file error, got %v", err)
	}
}

func TestConnectionKinds(t *testing.T) {
	c, err := Decode(strings.NewReader(`servers:
  prod:
    host: ops@203.0.113.10
  explicit:
    connection: remote
    host: ops@203.0.113.11
  self:
    connection: local
    user: ops
  self-default:
    connection: local
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Servers["prod"].Connection; got != ConnectionRemote {
		t.Fatalf("default connection = %q, want remote", got)
	}
	if c.Servers["prod"].IsLocal() {
		t.Fatal("remote server reported as local")
	}
	if got := c.Servers["explicit"].Connection; got != ConnectionRemote {
		t.Fatalf("explicit remote connection = %q", got)
	}
	self := c.Servers["self"]
	if !self.IsLocal() || self.User != "ops" || self.Host != "" || self.IdentityFile != "" {
		t.Fatalf("local server = %+v", self)
	}
	if got := c.Servers["self-default"].User; got != "" {
		t.Fatalf("local default user = %q, want empty", got)
	}
}

func TestConnectionValidation(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"local host", "connection: local\n    host: ops@203.0.113.10", "host: not allowed with connection: local"},
		{"local identity", "connection: local\n    identity_file: /tmp/key", "identity_file: not allowed with connection: local"},
		{"remote user", "host: ops@203.0.113.10\n    user: ops", "user: only valid with connection: local"},
		{"bad connection", "connection: ssh", "want remote or local"},
		{"bad local user", "connection: local\n    user: Root", "user: invalid local admin user"},
		{"root local user", "connection: local\n    user: root", "user: invalid local admin user"},
		{"remote without host", "connection: remote", "host: want user@host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader("servers:\n  prod:\n    " + tc.doc + "\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestImageAppMayHaveNoSourcePath(t *testing.T) {
	app := record(t, `domains: [web.example.com]
upstream: web:3000
source:
  type: image
backup: {}
`)
	if app.Source.Path != "" {
		t.Fatalf("source.path = %q, want empty", app.Source.Path)
	}
}

func TestRecordYAMLIsCanonicalAndOmitsSourcePath(t *testing.T) {
	doc := `domains: [app.example.com]
upstream: app:8080
source:
  type: image
build:
  commands: |
    npm ci
    npm run build
deploy:
  maintenance: false
  commands: ./migrate
env:
  B: two
  A: one
`
	app := record(t, doc)
	record, err := app.RecordYAML()
	if err != nil {
		t.Fatal(err)
	}
	text := string(record)
	if strings.Contains(text, "#") {
		t.Fatalf("record contains a comment:\n%s", text)
	}
	if strings.Contains(text, "path:") {
		t.Fatalf("record contains source.path:\n%s", text)
	}
	for _, want := range []string{"domains:", "app.example.com", "build:", "npm ci", "deploy:", "maintenance: false", "./migrate", "env:", "A: one", "B: two"} {
		if !strings.Contains(text, want) {
			t.Errorf("record missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "A: one") > strings.Index(text, "B: two") {
		t.Errorf("env keys are not canonical:\n%s", text)
	}
	parsed, err := DecodeAppRecord(strings.NewReader(text))
	if err != nil {
		t.Fatalf("record does not round-trip: %v", err)
	}
	if parsed.Source.Path != "" {
		t.Fatalf("parsed source.path = %q", parsed.Source.Path)
	}
}

func TestDecodeAppRecordRejectsUnknownFields(t *testing.T) {
	doc := `domains: [app.example.com]
upstream: app:8080
source: {type: github-release, repo: example/app}
mystery: value
`
	if _, err := DecodeAppRecord(strings.NewReader(doc)); err == nil {
		t.Fatal("record with an unknown field was accepted")
	}
}

func TestBuildApprovalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	hash := HashBuildCommands("npm ci\n")
	if got, err := ApprovedBuildHash(dir, "prod", "app"); err != nil || got != "" {
		t.Fatalf("empty approvals = %q, %v", got, err)
	}
	if err := RecordBuildApproval(dir, "prod", "app", hash); err != nil {
		t.Fatal(err)
	}
	got, err := ApprovedBuildHash(dir, "prod", "app")
	if err != nil {
		t.Fatal(err)
	}
	if got != hash {
		t.Fatalf("approved hash = %q, want %q", got, hash)
	}
}

func TestBuildDirsParseAndNormalize(t *testing.T) {
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    sources:
      web: https://github.com/acme/web.git
    build_dirs:
      web: docs/
`
	c, err := decode(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := server(t, c, "prod").BuildDirs["web"]; got != "docs" {
		t.Fatalf("build_dirs.web = %q", got)
	}
	bad := strings.Replace(doc, "web: docs/", "web: ../docs", 1)
	if _, err := decode(t, bad); err == nil || !strings.Contains(err.Error(), "build_dirs.web") {
		t.Fatalf("bad build directory error = %v", err)
	}
}

func TestLocalBuildDirEditing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    sources:
      web: https://github.com/acme/web.git
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetLocalSourceEntry(path, "prod", "web", "https://github.com/acme/web.git", "docs/", false); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Servers["prod"].BuildDirs["web"]; got != "docs" {
		t.Fatalf("build_dirs.web = %q", got)
	}
	// Changing only the build directory leaves the source scalar as written.
	if err := SetLocalBuildDir(path, "prod", "web", "site"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "web: https://github.com/acme/web.git") {
		t.Fatalf("source changed unexpectedly:\n%s", data)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Servers["prod"].BuildDirs["web"]; got != "site" {
		t.Fatalf("build_dirs.web = %q", got)
	}
	if err := SetLocalSource(path, "prod", "web", "", true); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Servers["prod"].BuildDirs["web"]; ok {
		t.Fatal("build directory was not removed with the source")
	}
}
