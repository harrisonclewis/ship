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
        uid: 10001
        data: [/srv/lewbox]
        health:
          http_path: /v0/health
        backup:
          sqlite: [/srv/lewbox/lewbox.db]
          files: [/srv/lewbox/keyring.key]
        env:
          LEWBOX_TRUSTED_PROXIES: 172.30.0.0/24
      commerce:
        domains: [example.com]
        redirects: [www.example.com]
        upstream: commerce:3000
        source:
          type: image
          path: ~/commerce
        health:
          http_path: /
        backup: {}
        env: {}
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
	if got := srv.AppNames(); strings.Join(got, ",") != "commerce,lewbox" {
		t.Fatalf("AppNames = %v", got)
	}
	if got := srv.PlatformArch(); got != "amd64" {
		t.Fatalf("PlatformArch = %q", got)
	}
	if got := srv.ResolveAsset("lewbox"); got != "lewbox_linux_amd64" {
		t.Fatalf("ResolveAsset = %q", got)
	}
	if got := srv.ImageRepo("commerce"); got != "ship/commerce" {
		t.Fatalf("ImageRepo = %q", got)
	}
	if got := srv.ImageRepo("lewbox"); got != "lewbox" {
		t.Fatalf("release ImageRepo = %q", got)
	}
	if got := srv.AppsSpec(); got != "lewbox:10001" {
		t.Fatalf("AppsSpec = %q", got)
	}
	home, _ := os.UserHomeDir()
	if got := c.BackupDirLocal; got != filepath.Join(home, "ship-backups") {
		t.Fatalf("BackupDirLocal = %q", got)
	}
}

func TestDecodeUnknownField(t *testing.T) {
	if _, err := decode(t, validYAML+"bogus: 1\n"); err == nil {
		t.Fatal("unknown field accepted")
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
	if !strings.Contains(err.Error(), "servers.<name>") {
		t.Fatalf("error does not explain the move: %v", err)
	}
}

func TestDefaults(t *testing.T) {
	doc := `servers:
  prod:
    host: root@203.0.113.10
    apps:
      lewbox:
        domains: [cloud.example.com]
        upstream: lewbox:8080
        source:
          type: github-release
          repo: harlew-dev/lewbox
`
	c, err := decode(t, doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	srv := server(t, c, "prod")
	if srv.AdminUser != "ops" || srv.Arch != "linux/amd64" || srv.CaddyImage != "caddy:2.10-alpine" {
		t.Fatalf("defaults not applied: %+v", srv)
	}
	if srv.Apps["lewbox"].Source.Asset != "{app}_linux_{arch}" {
		t.Fatalf("default asset = %q", srv.Apps["lewbox"].Source.Asset)
	}
	if !strings.HasSuffix(c.BackupDirLocal, filepath.Join("backups", "vps")) {
		t.Fatalf("default local backup dir = %q", c.BackupDirLocal)
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"bad host", strings.Replace(validYAML, "ops@203.0.113.10", "ops 203.0.113.10", 1), "host"},
		{"root admin", strings.Replace(validYAML, "admin_user: ops", "admin_user: root", 1), "admin_user"},
		{"uppercase app", strings.Replace(validYAML, "      lewbox:", "      Lewbox:", 1), "apps."},
		{"reserved app", strings.Replace(validYAML, "      commerce:", "      all:", 1), "reserved"},
		{"uppercase domain", strings.Replace(validYAML, "cloud.example.com", "Cloud.example.com", 1), "lowercase"},
		{"domain underscore", strings.Replace(validYAML, "cloud.example.com", "cloud_example.com", 1), "domain"},
		{"duplicate domain", strings.Replace(validYAML, "domains: [example.com]", "domains: [cloud.example.com]", 1), "already used"},
		{"duplicate redirect", strings.Replace(validYAML, "redirects: [www.example.com]", "redirects: [cloud.example.com]", 1), "already used"},
		{"bad upstream", strings.Replace(validYAML, "lewbox:8080", "lewbox", 1), "upstream"},
		{"bad upstream port", strings.Replace(validYAML, "lewbox:8080", "lewbox:70000", 1), "port"},
		{"bad repo", strings.Replace(validYAML, "repo: harlew-dev/lewbox", "repo: lewbox", 1), "repo"},
		{"relative image path", strings.Replace(validYAML, "path: ~/commerce", "path: commerce", 1), "absolute"},
		{"image with repo", strings.Replace(validYAML, "type: image\n          path: ~/commerce", "type: image\n          path: ~/commerce\n          repo: x/y", 1), "repo/asset"},
		{"sqlite without uid", strings.Replace(validYAML, "        uid: 10001\n", "", 1), "uid is required"},
		{"path traversal", strings.Replace(validYAML, "data: [/srv/lewbox]", "data: [/srv/../etc]", 1), "clean absolute"},
		{"env dollar", strings.Replace(validYAML, "172.30.0.0/24", "$SECRET", 1), "env."},
		{"reserved env", strings.Replace(validYAML, "LEWBOX_TRUSTED_PROXIES:", "IMAGE:", 1), "reserved"},
		{"bad health path", strings.Replace(validYAML, "http_path: /v0/health", "http_path: v0/health", 1), "http_path"},
		{"unpinned caddy", strings.Replace(validYAML, "caddy_image: caddy:2.10-alpine", "caddy_image: caddy", 1), "pin"},
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
	doc := strings.Split(validYAML, "    apps:")[0] + "    apps: {}\n"
	c, err := decode(t, doc)
	if err != nil {
		t.Fatalf("server without apps rejected: %v", err)
	}
	if got := server(t, c, "prod").AppNames(); len(got) != 0 {
		t.Fatalf("apps = %v, want none", got)
	}
}

func TestServerNamesAreReservedCommandWords(t *testing.T) {
	for _, name := range []string{"setup", "deploy", "status", "version", "update", "help", "all", "proxy"} {
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

func TestServerNameRules(t *testing.T) {
	for _, name := range []string{"Prod", "p", "1prod", "prod_server"} {
		doc := strings.Replace(validYAML, "  prod:", "  "+name+":", 1)
		if _, err := decode(t, doc); err == nil {
			t.Errorf("server name %q accepted", name)
		}
	}
}

func TestDomainsMustBeUniqueAcrossServers(t *testing.T) {
	doc := validYAML + `  staging:
    host: ops@203.0.113.11
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
		t.Fatal("cross-server duplicate domain accepted")
	}
	if !strings.Contains(err.Error(), "already used") {
		t.Fatalf("error = %v", err)
	}
}

func TestSameAppNameOnDifferentServers(t *testing.T) {
	doc := validYAML + `  staging:
    host: ops@203.0.113.11
    apps:
      lewbox:
        domains: [staging.example.com]
        upstream: lewbox:8080
        source:
          type: github-release
          repo: harlew-dev/lewbox
        uid: 10001
        backup:
          sqlite: [/srv/lewbox/lewbox.db]
`
	c, err := decode(t, doc)
	if err != nil {
		t.Fatalf("same app name on two servers rejected: %v", err)
	}
	if got := server(t, c, "staging").AppNames(); strings.Join(got, ",") != "lewbox" {
		t.Fatalf("staging apps = %v", got)
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
	want := filepath.Join(dir, "apps", "commerce", "compose.yaml")
	if got := srv.ComposePath("commerce"); got != want {
		t.Fatalf("ComposePath = %q, want %q", got, want)
	}
	want = filepath.Join(dir, "apps", "lewbox", "Dockerfile")
	if got := srv.DockerfilePath("lewbox"); got != want {
		t.Fatalf("DockerfilePath = %q, want %q", got, want)
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
	if got := server(t, c, "prod").ComposePath("commerce"); got != filepath.Join(dir, "apps", "commerce", "compose.yaml") {
		t.Fatalf("ComposePath = %q", got)
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
	c, err := decode(t, validYAML)
	if err != nil {
		t.Fatal(err)
	}
	srv := server(t, c, "prod")
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
}

func TestIdentityFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	withKey := strings.Replace(validYAML, "    admin_user: ops\n", "    identity_file: ~/.ssh/prod\n    admin_user: ops\n", 1)
	c, err := Decode(strings.NewReader(withKey))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Servers["prod"].IdentityFile, filepath.Join(home, ".ssh", "prod"); got != want {
		t.Fatalf("identity_file = %q, want %q", got, want)
	}
	relative := strings.Replace(validYAML, "    admin_user: ops\n", "    identity_file: keys/prod\n    admin_user: ops\n", 1)
	if _, err := Decode(strings.NewReader(relative)); err == nil || !strings.Contains(err.Error(), "identity_file") {
		t.Fatalf("relative identity_file: want an identity_file error, got %v", err)
	}
}
