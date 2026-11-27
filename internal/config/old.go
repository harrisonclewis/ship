package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// LoadOld reads the old full-format ship.yaml for `ship config push`. It is the
// only caller that accepts app blocks, admin_user, arch and caddy_image in the
// local file.
func LoadOld(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c, err := DecodeOld(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.Path = abs
	c.setDir(filepath.Dir(abs))
	return c, nil
}

// DecodeOld parses the old full-format document (used by migration and the
// still-old-format TUI editors).
func DecodeOld(r io.Reader) (*Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if err == io.EOF {
			return &Config{}, nil
		}
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%s must contain exactly one YAML document", ConfigName)
		}
		return nil, err
	}
	if err := c.validateOld(); err != nil {
		return nil, err
	}
	return &c, nil
}

// validateOld validates an old-format document and applies the defaults the
// old loader used, so migration reads exactly what the previous ship saw.
func (c *Config) validateOld() error {
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
		return fmt.Errorf("backup_dir_local: control characters are not allowed")
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
		if err := srv.validateOld(seenHost); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) validateOld(seenHost map[string]string) error {
	p := "servers." + s.Name
	switch s.Connection {
	case "":
		s.Connection = ConnectionRemote
	case ConnectionRemote, ConnectionLocal:
	default:
		return fmt.Errorf("%s.connection: want remote or local, got %q", p, s.Connection)
	}
	if s.IsLocal() {
		if s.Host != "" {
			return fmt.Errorf("%s.host: not allowed with connection: local", p)
		}
		if s.IdentityFile != "" {
			return fmt.Errorf("%s.identity_file: not allowed with connection: local", p)
		}
		if s.User != "" && (!adminUserRE.MatchString(s.User) || s.User == "root" || s.User == "deploy") {
			return fmt.Errorf("%s.user: invalid local admin user %q (want a name like ops)", p, s.User)
		}
	} else {
		if s.User != "" {
			return fmt.Errorf("%s.user: only valid with connection: local", p)
		}
		if !userHostRE.MatchString(s.Host) {
			return fmt.Errorf("%s.host: want user@host (for example ops@203.0.113.10), got %q", p, s.Host)
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
	}
	if s.AdminUser == "" {
		s.AdminUser = DefaultAdminUser
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
