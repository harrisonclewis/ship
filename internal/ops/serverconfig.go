package ops

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
	"github.com/harrisonclewis/ship/internal/render"
)

const (
	configFileLimit  = 1 << 20  // one desired/running file
	configTotalLimit = 32 << 20 // the whole config export
)

// ErrConfigConflict reports that desired configuration changed on the server
// after the caller loaded it. The write was refused; reload and try again.
var ErrConfigConflict = errors.New("configuration changed on the server since it was loaded")

// conflictMarker is the agent's wording for a stale expected hash.
const conflictMarker = "changed on the server since you loaded"

// ServerSettings is the desired server-wide configuration.
type ServerSettings struct {
	AdminUser  string
	CaddyImage string
	Hash       string
	// Present is false on a server that has never had server settings saved.
	Present bool
}

// DesiredApp is one app's authoritative configuration on the server.
type DesiredApp struct {
	Name          string
	Record        *config.App
	RecordErr     string
	Compose       []byte
	Dockerfile    []byte
	HasDockerfile bool
	Hash          string
}

// RunningApp is one app's last promoted (running) configuration.
type RunningApp struct {
	Name          string
	Record        *config.App
	RecordErr     string
	Compose       []byte
	Dockerfile    []byte
	HasDockerfile bool
	Meta          map[string]string
	Current       string
	Previous      string
	CurrentRef    string
	PreviousRef   string
}

// AppliedSetup records the admin user and app:uid list the last setup applied.
type AppliedSetup struct {
	Present   bool
	AdminUser string
	Apps      string
}

// AppliedProxy records the Caddyfile and image the last proxy operation used.
type AppliedProxy struct {
	Present      bool
	CaddyfileSHA string
	CaddyImage   string
	Mode         string
}

// ServerConfig is the desired and running configuration read from a server.
type ServerConfig struct {
	Server          string
	Settings        ServerSettings
	Apps            map[string]*DesiredApp
	Running         map[string]*RunningApp
	AppliedSetup    AppliedSetup
	AppliedProxy    AppliedProxy
	ActiveCaddyfile []byte
	DesiredImage    string
	RunningImage    string
	Arch            string
}

// RunningWithoutDesired returns apps that have running files but no desired
// record. They would disappear from a Caddyfile rendered only from desired
// records, so proxy operations refuse until the server is migrated.
func (c *ServerConfig) RunningWithoutDesired() []string {
	var out []string
	for name, ra := range c.Running {
		if c.Apps[name] != nil {
			continue
		}
		if ra.Record != nil || len(ra.Meta) > 0 {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// AppNames returns desired app names in sorted order.
func (c *ServerConfig) AppNames() []string {
	names := make([]string, 0, len(c.Apps))
	for name := range c.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FetchConfig reads desired and running configuration from a server. It reads
// over ssh even under --dry-run; the agent is only updated when not dry-run.
func (o *Ops) FetchConfig(ctx context.Context, server string) (*ServerConfig, error) {
	s, srv, err := o.readSession(ctx, server)
	if err != nil {
		return nil, err
	}
	if !o.opts.DryRun {
		if err := o.ensureAgent(s); err != nil {
			return nil, err
		}
	}
	var out limitBuffer
	out.limit = configTotalLimit
	if err := s.StreamAgent(&out, "config-export"); err != nil {
		if looksLikeAgentUsage(out.Bytes()) {
			return nil, olderAgentError(srv.Name)
		}
		return nil, fmt.Errorf("read configuration from %s: %w", srv.Name, err)
	}
	if looksLikeAgentUsage(out.Bytes()) {
		return nil, olderAgentError(srv.Name)
	}
	if out.overflow {
		return nil, fmt.Errorf("configuration on %s is larger than %d bytes", srv.Name, configTotalLimit)
	}
	state, err := parseConfigExport(out.Bytes())
	if err != nil {
		return nil, err
	}
	state.Server = srv.Name
	state.applyDefaults()
	return state, nil
}

func (c *ServerConfig) applyDefaults() {
	if c.Settings.AdminUser == "" {
		c.Settings.AdminUser = config.DefaultAdminUser
	}
	if c.Settings.CaddyImage == "" {
		c.Settings.CaddyImage = config.DefaultCaddyImage
	}
	if c.Arch == "" {
		c.Arch = strings.TrimPrefix(config.DefaultArch, "linux/")
	}
}

func parseConfigExport(data []byte) (*ServerConfig, error) {
	state := &ServerConfig{
		Apps:    map[string]*DesiredApp{},
		Running: map[string]*RunningApp{},
	}
	tr := tar.NewReader(bytes.NewReader(data))
	seen := map[string]bool{}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("config export: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("config export: %q is not a regular file", hdr.Name)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name == "" || strings.ContainsAny(name, `\`) {
			return nil, fmt.Errorf("config export: invalid member name %q", hdr.Name)
		}
		if seen[name] {
			return nil, fmt.Errorf("config export: duplicate member %q", name)
		}
		seen[name] = true
		if hdr.Size < 0 || hdr.Size > configFileLimit {
			return nil, fmt.Errorf("config export: %q is larger than %d bytes", name, configFileLimit)
		}
		total += hdr.Size
		if total > configTotalLimit {
			return nil, fmt.Errorf("config export is larger than %d bytes", configTotalLimit)
		}
		body, err := io.ReadAll(io.LimitReader(tr, configFileLimit+1))
		if err != nil {
			return nil, fmt.Errorf("config export: read %q: %w", name, err)
		}
		if int64(len(body)) != hdr.Size {
			return nil, fmt.Errorf("config export: %q has the wrong size", name)
		}
		if err := state.accept(name, body); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (c *ServerConfig) accept(name string, body []byte) error {
	switch name {
	case "config/server.yaml":
		var doc struct {
			AdminUser  string `yaml:"admin_user"`
			CaddyImage string `yaml:"caddy_image"`
		}
		if err := strictYAML(body, &doc); err != nil {
			return fmt.Errorf("server settings: %w", err)
		}
		if err := config.ValidateServerSettings(doc.AdminUser, doc.CaddyImage); err != nil {
			return err
		}
		hash := c.Settings.Hash
		c.Settings = ServerSettings{AdminUser: doc.AdminUser, CaddyImage: doc.CaddyImage, Present: true, Hash: hash}
		return nil
	case "config/applied-setup.yaml":
		kv := parseKV(body)
		c.AppliedSetup = AppliedSetup{Present: true, AdminUser: kv["admin_user"], Apps: kv["apps"]}
		return nil
	case "config/applied-proxy.yaml":
		kv := parseKV(body)
		c.AppliedProxy = AppliedProxy{Present: true, CaddyfileSHA: kv["caddyfile_sha256"], CaddyImage: kv["caddy_image"], Mode: kv["mode"]}
		return nil
	case "config/arch":
		c.Arch = strings.TrimSpace(string(body))
		return nil
	case "config/server.hash":
		c.Settings.Hash = strings.TrimSpace(string(body))
		return nil
	case "proxy/Caddyfile":
		c.ActiveCaddyfile = body
		return nil
	case "proxy/desired-image":
		c.DesiredImage = strings.TrimSpace(string(body))
		return nil
	case "proxy/running-image":
		c.RunningImage = strings.TrimSpace(string(body))
		return nil
	}

	parts := strings.Split(name, "/")
	if len(parts) != 3 {
		return fmt.Errorf("config export: unexpected member %q", name)
	}
	app := parts[1]
	if err := config.ValidateAppName(app); err != nil {
		return fmt.Errorf("config export: invalid app directory %q", app)
	}
	switch parts[0] {
	case "desired":
		da := c.Apps[app]
		if da == nil {
			da = &DesiredApp{Name: app}
			c.Apps[app] = da
		}
		switch parts[2] {
		case "ship-app.yaml":
			rec, err := config.DecodeAppRecord(bytes.NewReader(body))
			if err != nil {
				da.RecordErr = err.Error()
				return nil
			}
			da.Record = rec
		case "compose.yaml":
			da.Compose = body
		case "Dockerfile":
			da.Dockerfile = body
			da.HasDockerfile = true
		case ".hash":
			da.Hash = strings.TrimSpace(string(body))
		default:
			return fmt.Errorf("config export: unexpected desired file %q", name)
		}
	case "running":
		ra := c.Running[app]
		if ra == nil {
			ra = &RunningApp{Name: app, Meta: map[string]string{}}
			c.Running[app] = ra
		}
		switch parts[2] {
		case "ship-app.yaml":
			rec, err := config.DecodeAppRecord(bytes.NewReader(body))
			if err != nil {
				ra.RecordErr = err.Error()
				return nil
			}
			ra.Record = rec
		case "compose.yaml":
			ra.Compose = body
		case "Dockerfile":
			ra.Dockerfile = body
			ra.HasDockerfile = true
		case ".ship-meta":
			ra.Meta = parseKV(body)
		case ".ship-state":
			kv := parseKV(body)
			ra.Current = kv["current"]
			ra.Previous = kv["previous"]
			ra.CurrentRef = kv["current_ref"]
			ra.PreviousRef = kv["previous_ref"]
		default:
			return fmt.Errorf("config export: unexpected running file %q", name)
		}
	default:
		return fmt.Errorf("config export: unexpected member %q", name)
	}
	return nil
}

func parseKV(data []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// PutDesiredApp validates and uploads one app's desired configuration. The
// expected hash is the one FetchConfig returned when the caller loaded it; the
// agent rejects a mismatch instead of overwriting a newer change.
func (o *Ops) PutDesiredApp(ctx context.Context, server, app string, record *config.App, compose, dockerfile []byte, arch, expectedHash string) (string, error) {
	if err := config.ValidateAppName(app); err != nil {
		return "", err
	}
	if record == nil {
		return "", fmt.Errorf("app %s has no record", app)
	}
	if record.Source.Path != "" {
		return "", fmt.Errorf("app %s: source.path is local and cannot be stored on the server", app)
	}
	if err := record.Validate(); err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(compose)) == 0 {
		return "", fmt.Errorf("app %s: compose.yaml is empty", app)
	}
	if err := config.ValidateCompose(compose); err != nil {
		return "", fmt.Errorf("app %s compose.yaml: %w", app, err)
	}
	recordYAML, err := record.RecordYAML()
	if err != nil {
		return "", err
	}
	platform, archErr := config.NormalizeArch(arch)
	if archErr != nil {
		platform = config.DefaultArch
	}
	metaServer := &config.Server{Name: server, Arch: platform, Apps: map[string]*config.App{app: record}}
	files := []appFile{
		{Name: "ship-app.yaml", Mode: 0o644, Data: recordYAML},
		{Name: "compose.yaml", Mode: 0o644, Data: compose},
		{Name: ".ship-meta", Mode: 0o644, Data: []byte(metaServer.Meta(app))},
		{Name: ".ship-env", Mode: 0o644, Data: []byte(metaServer.ShipEnv(app))},
		{Name: ".ship-deploy.sh", Mode: 0o644, Data: []byte(record.Deploy.Commands)},
	}
	if len(dockerfile) > 0 {
		files = append(files, appFile{Name: "Dockerfile", Mode: 0o644, Data: dockerfile})
	}
	blob, err := appFilesTar(files)
	if err != nil {
		return "", err
	}
	if o.opts.DryRun {
		fmt.Fprintf(o.opts.Out, "# would put desired configuration for %s on %s\n", app, server)
		return expectedHash, nil
	}
	s, err := o.configSession(ctx, server)
	if err != nil {
		return "", err
	}
	var out []byte
	err = captureAgentStderr(s, func() error {
		var e error
		out, e = s.InputAgentOutput(blob, "app-put", app, expectedHash)
		return e
	})
	if err != nil {
		return "", err
	}
	return parseAgentHash(out)
}

// captureAgentStderr collects the agent's stderr while a write runs so a stale
// expected hash can be reported as ErrConfigConflict. Other errors pass through
// unchanged and stderr is still written to the session's error writer.
func captureAgentStderr(s *remote.Session, run func() error) error {
	var stderr bytes.Buffer
	original := s.Err
	if original == nil {
		original = io.Discard
	}
	s.Err = io.MultiWriter(original, &stderr)
	err := run()
	s.Err = original
	if err == nil {
		return nil
	}
	if text := strings.TrimSpace(stderr.String()); strings.Contains(text, conflictMarker) {
		return fmt.Errorf("%w: %s", ErrConfigConflict, text)
	}
	return err
}

// PutServerSettings validates and uploads the desired server-wide settings.
func (o *Ops) PutServerSettings(ctx context.Context, server, adminUser, caddyImage, expectedHash string) (string, error) {
	if err := config.ValidateServerSettings(adminUser, caddyImage); err != nil {
		return "", err
	}
	doc := fmt.Sprintf("admin_user: %s\ncaddy_image: %s\n", adminUser, caddyImage)
	blob, err := singleFileTar("server.yaml", []byte(doc))
	if err != nil {
		return "", err
	}
	if o.opts.DryRun {
		fmt.Fprintf(o.opts.Out, "# would put server settings on %s\n", server)
		return expectedHash, nil
	}
	s, err := o.configSession(ctx, server)
	if err != nil {
		return "", err
	}
	var out []byte
	err = captureAgentStderr(s, func() error {
		var e error
		out, e = s.InputAgentOutput(blob, "server-put", expectedHash)
		return e
	})
	if err != nil {
		return "", err
	}
	return parseAgentHash(out)
}

// RemoveDesiredApp deletes an app's desired configuration and stops its stack.
func (o *Ops) RemoveDesiredApp(ctx context.Context, server, app, expectedHash string) error {
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	s, err := o.configSession(ctx, server)
	if err != nil {
		return err
	}
	return captureAgentStderr(s, func() error {
		return s.RunAgent("app-remove", app, expectedHash)
	})
}

func (o *Ops) configSession(ctx context.Context, server string) (*remote.Session, error) {
	s, _, err := o.session(ctx, server)
	return s, err
}

func strictYAML(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("must contain exactly one YAML document")
		}
		return err
	}
	return nil
}

func parseAgentHash(out []byte) (string, error) {
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "hash="); ok {
			v = strings.TrimSpace(v)
			if len(v) != 64 {
				return "", fmt.Errorf("agent returned an invalid hash")
			}
			return v, nil
		}
	}
	return "", fmt.Errorf("agent did not return a configuration hash")
}

func singleFileTar(name string, data []byte) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(data); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DesiredRecords returns the desired records keyed by app, failing on any
// unreadable record.
func (c *ServerConfig) DesiredRecords() (map[string]*config.App, error) {
	out := make(map[string]*config.App, len(c.Apps))
	for _, name := range c.AppNames() {
		da := c.Apps[name]
		if da.Record == nil {
			reason := "the record is missing"
			if da.RecordErr != "" {
				reason = da.RecordErr
			}
			return nil, fmt.Errorf("app %s: %s", name, reason)
		}
		out[name] = da.Record
	}
	return out, nil
}

// AppsSpecFor builds the app:uid list passed to ship-agent setup from desired
// records.
func AppsSpecFor(apps map[string]*config.App) string {
	names := make([]string, 0, len(apps))
	for name := range apps {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		if uid := apps[name].UID; uid != nil {
			parts = append(parts, name+":"+strconv.Itoa(*uid))
		}
	}
	return strings.Join(parts, ",")
}

// CaddyfileFor renders the server's live routes: desired domains and redirects,
// running upstreams, and no entry for an app that was never deployed.
func (c *ServerConfig) CaddyfileFor() string {
	names := make([]string, 0, len(c.Apps))
	for name := range c.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]render.App, 0, len(names))
	for _, name := range names {
		da := c.Apps[name]
		ra := c.Running[name]
		entry := render.App{Name: name, Deployed: ra != nil && ra.Record != nil}
		if da.Record != nil {
			entry.Domains = da.Record.Domains
			entry.Redirects = da.Record.Redirects
			entry.Upstream = da.Record.Upstream
		}
		if ra != nil && ra.Record != nil {
			entry.Upstream = ra.Record.Upstream
		}
		entries = append(entries, entry)
	}
	return render.CaddyfileFor(entries)
}
