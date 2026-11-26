package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/harrisonclewis/ship/internal/config"
)

// ErrConfigChanged means ship.yaml no longer matches what the TUI loaded.
var ErrConfigChanged = errors.New("ship.yaml changed on disk since it was loaded; reopen the TUI to reload it")

type yamlDoc struct {
	path     string
	original []byte
	exists   bool
	mode     os.FileMode
	root     yaml.Node
}

func loadYAMLDoc(path string) (*yamlDoc, error) {
	d := &yamlDoc{path: path, mode: 0o600}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return d, nil
		}
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	d.original = data
	d.exists = true
	d.mode = fi.Mode().Perm()
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &d.root); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return d, nil
}

func newEmptyYAMLDoc(path string) *yamlDoc {
	return &yamlDoc{path: path, mode: 0o600}
}

func (d *yamlDoc) mapping() *yaml.Node {
	if d.root.Kind == 0 {
		d.root = yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(d.root.Content) == 0 {
		d.root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	node := d.root.Content[0]
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
		node.Value = ""
		node.Style = 0
	}
	return node
}

func (d *yamlDoc) clone() *yamlDoc {
	out := &yamlDoc{
		path:     d.path,
		original: append([]byte(nil), d.original...),
		exists:   d.exists,
		mode:     d.mode,
	}
	out.root = *cloneNode(&d.root)
	return out
}

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	out := *n
	if len(n.Content) > 0 {
		out.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			out.Content[i] = cloneNode(child)
		}
	}
	return &out
}

func (d *yamlDoc) bytes() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&d.root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return restoreBlankLines(d.original, buf.Bytes()), nil
}

// restoreBlankLines puts back the blank lines the YAML encoder drops: a line
// that appears in the original file after a blank line gets one again. The
// result is used only if it decodes to the same data, so a blank line can
// never be added inside a block scalar such as deploy commands.
func restoreBlankLines(original, rendered []byte) []byte {
	if len(original) == 0 {
		return rendered
	}
	orig := strings.Split(string(original), "\n")
	var out []string
	pos := 0
	for _, line := range strings.Split(string(rendered), "\n") {
		if strings.TrimSpace(line) != "" {
			for j := pos; j < len(orig); j++ {
				if orig[j] != line {
					continue
				}
				if j > 0 && strings.TrimSpace(orig[j-1]) == "" &&
					len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
					out = append(out, "")
				}
				pos = j + 1
				break
			}
		}
		out = append(out, line)
	}
	restored := []byte(strings.Join(out, "\n"))
	var a, b any
	if yaml.Unmarshal(rendered, &a) != nil || yaml.Unmarshal(restored, &b) != nil || !reflect.DeepEqual(a, b) {
		return rendered
	}
	return restored
}

// save validates the whole document, checks that the file on disk is still the
// version this TUI loaded, then replaces it atomically and keeps a .bak copy.
func (d *yamlDoc) save() error {
	rendered, err := d.bytes()
	if err != nil {
		return err
	}
	if _, err := config.Decode(bytes.NewReader(rendered)); err != nil {
		return err
	}

	if d.exists {
		current, err := os.ReadFile(d.path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, d.original) {
			return ErrConfigChanged
		}
	} else if _, err := os.Stat(d.path); err == nil {
		return ErrConfigChanged
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := d.mode
	if mode == 0 {
		mode = 0o600
	}
	if !d.exists {
		mode = 0o600
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(d.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if err := f.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if _, err := f.Write(rendered); err != nil {
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if d.exists && len(d.original) > 0 {
		bak := d.path + ".bak"
		if err := os.WriteFile(bak, d.original, mode); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("write %s: %w", bak, err)
		}
	}
	if err := os.Rename(tmp, d.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	d.original = rendered
	d.exists = true
	d.mode = mode
	return nil
}

func lookupKey(m *yaml.Node, key string) (value *yaml.Node, index int) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], i
		}
	}
	return nil, -1
}

func appendKey(m *yaml.Node, key string, value *yaml.Node) *yaml.Node {
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	return value
}

func ensureMap(m *yaml.Node, key string) *yaml.Node {
	if v, _ := lookupKey(m, key); v != nil {
		if v.Kind != yaml.MappingNode {
			v.Kind = yaml.MappingNode
			v.Tag = "!!map"
			v.Value = ""
			v.Style = 0
			v.Content = nil
		}
		return v
	}
	return appendKey(m, key, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
}

func removeKey(m *yaml.Node, key string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func setString(m *yaml.Node, key, value string) {
	v, _ := lookupKey(m, key)
	if v == nil {
		v = appendKey(m, key, &yaml.Node{})
	}
	v.Kind = yaml.ScalarNode
	v.Tag = "!!str"
	v.Value = value
	v.Style = 0
	v.Content = nil
}

func setLiteral(m *yaml.Node, key, value string) {
	v, _ := lookupKey(m, key)
	if v == nil {
		v = appendKey(m, key, &yaml.Node{})
	}
	v.Kind = yaml.ScalarNode
	v.Tag = "!!str"
	v.Style = yaml.LiteralStyle
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	v.Value = value
	v.Content = nil
}

func setBool(m *yaml.Node, key string, value bool) {
	v, _ := lookupKey(m, key)
	if v == nil {
		v = appendKey(m, key, &yaml.Node{})
	}
	v.Kind = yaml.ScalarNode
	v.Tag = "!!bool"
	v.Value = strconv.FormatBool(value)
	v.Style = 0
	v.Content = nil
}

func setInt(m *yaml.Node, key string, value int) {
	v, _ := lookupKey(m, key)
	if v == nil {
		v = appendKey(m, key, &yaml.Node{})
	}
	v.Kind = yaml.ScalarNode
	v.Tag = "!!int"
	v.Value = strconv.Itoa(value)
	v.Style = 0
	v.Content = nil
}

func setStringSeq(m *yaml.Node, key string, values []string) {
	if len(values) == 0 {
		removeKey(m, key)
		return
	}
	v, _ := lookupKey(m, key)
	if v == nil {
		v = appendKey(m, key, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle})
	}
	v.Kind = yaml.SequenceNode
	v.Tag = "!!seq"
	v.Value = ""
	v.Content = v.Content[:0]
	for _, item := range values {
		v.Content = append(v.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: item})
	}
}

func setMapping(m *yaml.Node, key string) *yaml.Node {
	v, _ := lookupKey(m, key)
	if v == nil {
		v = appendKey(m, key, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	}
	v.Kind = yaml.MappingNode
	v.Tag = "!!map"
	v.Value = ""
	v.Content = v.Content[:0]
	return v
}

func isEmptyMapping(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.MappingNode && len(n.Content) == 0
}

func (d *yamlDoc) serversMap() *yaml.Node {
	return ensureMap(d.mapping(), "servers")
}

func (d *yamlDoc) serverNode(name string) *yaml.Node {
	v, _ := lookupKey(d.serversMap(), name)
	return v
}

func (d *yamlDoc) applyServer(v serverFormValues) error {
	servers := d.serversMap()
	oldName := v.OriginalName
	if oldName == "" {
		oldName = v.Name
	}
	_, oldIndex := lookupKey(servers, oldName)
	_, newIndex := lookupKey(servers, v.Name)
	if v.IsNew {
		if newIndex >= 0 {
			return fmt.Errorf("server %q already exists", v.Name)
		}
	} else if oldIndex < 0 {
		return fmt.Errorf("server %q is no longer in ship.yaml", oldName)
	} else if oldName != v.Name && newIndex >= 0 {
		return fmt.Errorf("server %q already exists", v.Name)
	}

	var srv *yaml.Node
	if oldIndex >= 0 {
		srv = servers.Content[oldIndex+1]
		if oldName != v.Name {
			servers.Content[oldIndex].Value = v.Name
		}
	} else {
		srv = appendKey(servers, v.Name, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	}
	if srv.Kind != yaml.MappingNode {
		srv.Kind = yaml.MappingNode
		srv.Tag = "!!map"
		srv.Value = ""
		srv.Content = nil
	}

	if v.Host != "" {
		setString(srv, "host", v.Host)
	} else {
		setString(srv, "host", "")
	}
	applyIdentityFile(srv, strings.TrimSpace(v.IdentityFile))
	if v.AdminUser != "" {
		setString(srv, "admin_user", v.AdminUser)
	} else {
		removeKey(srv, "admin_user")
	}
	if v.Arch != "" {
		setString(srv, "arch", v.Arch)
	} else {
		removeKey(srv, "arch")
	}
	if v.CaddyImage != "" {
		setString(srv, "caddy_image", v.CaddyImage)
	} else {
		removeKey(srv, "caddy_image")
	}
	if v.IsNew {
		ensureMap(srv, "apps")
	}
	return nil
}

func (d *yamlDoc) deleteServer(name string) error {
	servers := d.serversMap()
	_, index := lookupKey(servers, name)
	if index < 0 {
		return fmt.Errorf("server %q is no longer in ship.yaml", name)
	}
	servers.Content = append(servers.Content[:index], servers.Content[index+2:]...)
	return nil
}

func (d *yamlDoc) appsMap(server string) (*yaml.Node, error) {
	srv := d.serverNode(server)
	if srv == nil {
		return nil, fmt.Errorf("server %q is no longer in ship.yaml", server)
	}
	return ensureMap(srv, "apps"), nil
}

func (d *yamlDoc) applyApp(v appFormValues) error {
	apps, err := d.appsMap(v.Server)
	if err != nil {
		return err
	}
	oldName := v.OriginalName
	if oldName == "" {
		oldName = v.Name
	}
	_, oldIndex := lookupKey(apps, oldName)
	_, newIndex := lookupKey(apps, v.Name)
	if v.IsNew {
		if newIndex >= 0 {
			return fmt.Errorf("app %q already exists on %s", v.Name, v.Server)
		}
	} else if oldIndex < 0 {
		return fmt.Errorf("app %q is no longer in ship.yaml", oldName)
	} else if oldName != v.Name && newIndex >= 0 {
		return fmt.Errorf("app %q already exists on %s", v.Name, v.Server)
	}

	var app *yaml.Node
	if oldIndex >= 0 {
		app = apps.Content[oldIndex+1]
		if oldName != v.Name {
			apps.Content[oldIndex].Value = v.Name
		}
	} else {
		app = appendKey(apps, v.Name, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	}
	if app.Kind != yaml.MappingNode {
		app.Kind = yaml.MappingNode
		app.Tag = "!!map"
		app.Value = ""
		app.Content = nil
	}

	setStringSeq(app, "domains", v.Domains)
	setStringSeq(app, "redirects", v.Redirects)
	setString(app, "upstream", v.Upstream)

	source := ensureMap(app, "source")
	setString(source, "type", v.SourceType)
	switch v.SourceType {
	case "image":
		setString(source, "path", v.SourcePath)
		removeKey(source, "repo")
		removeKey(source, "asset")
	default:
		setString(source, "repo", v.SourceRepo)
		if v.SourceAsset != "" {
			setString(source, "asset", v.SourceAsset)
		} else {
			removeKey(source, "asset")
		}
		removeKey(source, "path")
	}

	if v.UID != "" {
		uid, err := strconv.Atoi(strings.TrimSpace(v.UID))
		if err != nil {
			return fmt.Errorf("uid: %q is not a number", v.UID)
		}
		setInt(app, "uid", uid)
	} else {
		removeKey(app, "uid")
	}
	setStringSeq(app, "data", v.Data)

	if v.HealthPath != "" {
		health := ensureMap(app, "health")
		setString(health, "http_path", v.HealthPath)
	} else {
		removeKey(app, "health")
	}

	applyBackup(app, v)

	if err := applyEnv(app, v.Env); err != nil {
		return err
	}

	if v.SourceType == "image" && strings.TrimSpace(v.BuildCommands) != "" {
		build := ensureMap(app, "build")
		setLiteral(build, "commands", v.BuildCommands)
	} else {
		removeKey(app, "build")
	}

	applyDeploy(app, v)

	return nil
}

func applyBackup(app *yaml.Node, v appFormValues) {
	existing, _ := lookupKey(app, "backup")
	existed := existing != nil
	needs := len(v.BackupSQLiteV) > 0 || len(v.BackupFilesV) > 0 ||
		v.PostgresService != "" || v.PostgresDB != "" || v.PostgresUser != ""
	if !needs && !existed {
		return
	}
	var backup *yaml.Node
	if needs {
		backup = ensureMap(app, "backup")
	} else {
		backup = existing
	}
	if backup == nil {
		return
	}
	setStringSeq(backup, "sqlite", v.BackupSQLiteV)
	setStringSeq(backup, "files", v.BackupFilesV)
	if v.PostgresService != "" || v.PostgresDB != "" || v.PostgresUser != "" {
		pg := ensureMap(backup, "postgres")
		setString(pg, "service", v.PostgresService)
		setString(pg, "db", v.PostgresDB)
		setString(pg, "user", v.PostgresUser)
	} else {
		removeKey(backup, "postgres")
	}
	if existed && isEmptyMapping(backup) {
		return
	}
}

func applyEnv(app *yaml.Node, text string) error {
	values := map[string]string{}
	var order []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("env: %q is not KEY=VALUE", line)
		}
		k = strings.TrimSpace(k)
		if _, seen := values[k]; !seen {
			order = append(order, k)
		}
		values[k] = strings.TrimSpace(val)
	}
	if len(values) == 0 {
		removeKey(app, "env")
		return nil
	}
	env, _ := lookupKey(app, "env")
	if env == nil {
		env = appendKey(app, "env", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	}
	if env.Kind != yaml.MappingNode {
		env.Kind = yaml.MappingNode
		env.Tag = "!!map"
		env.Value = ""
		env.Content = nil
	}
	for _, k := range order {
		setString(env, k, values[k])
	}
	for i := 0; i+1 < len(env.Content); i += 2 {
		if _, keep := values[env.Content[i].Value]; !keep {
			env.Content = append(env.Content[:i], env.Content[i+2:]...)
			i -= 2
		}
	}
	return nil
}

func applyDeploy(app *yaml.Node, v appFormValues) {
	commands := strings.TrimSpace(v.DeployCommands) != ""
	maintenance := v.DeployMaintenance
	// Keep a maintenance key the user wrote, even when it says the default.
	explicit := false
	if existing, _ := lookupKey(app, "deploy"); existing != nil {
		if m, _ := lookupKey(existing, "maintenance"); m != nil {
			explicit = true
		}
	}
	if !commands && maintenance && !explicit {
		removeKey(app, "deploy")
		return
	}
	deploy := ensureMap(app, "deploy")
	if commands {
		setLiteral(deploy, "commands", v.DeployCommands)
	} else {
		removeKey(deploy, "commands")
	}
	if !maintenance || explicit {
		setBool(deploy, "maintenance", maintenance)
	} else {
		removeKey(deploy, "maintenance")
	}
}

func (d *yamlDoc) deleteApp(server, app string) error {
	apps, err := d.appsMap(server)
	if err != nil {
		return err
	}
	_, index := lookupKey(apps, app)
	if index < 0 {
		return fmt.Errorf("app %q is no longer in ship.yaml", app)
	}
	apps.Content = append(apps.Content[:index], apps.Content[index+2:]...)
	return nil
}

// appSourceRaw returns the source asset and path exactly as written. Config
// defaults and ~ expansion must not be baked back into the file just because
// the app was edited.
func (d *yamlDoc) appSourceRaw(server, app string) (asset, sourcePath string) {
	srv := d.serverNode(server)
	if srv == nil {
		return "", ""
	}
	apps, _ := lookupKey(srv, "apps")
	if apps == nil {
		return "", ""
	}
	appNode, _ := lookupKey(apps, app)
	if appNode == nil {
		return "", ""
	}
	source, _ := lookupKey(appNode, "source")
	if source == nil {
		return "", ""
	}
	if n, _ := lookupKey(source, "asset"); n != nil {
		asset = n.Value
	}
	if n, _ := lookupKey(source, "path"); n != nil {
		sourcePath = n.Value
	}
	return asset, sourcePath
}

// appExtras reads the fields added by the build/deploy contract straight from
// the YAML tree so the TUI form does not depend on their Go types.
func (d *yamlDoc) appExtras(server, app string) (buildCommands, deployCommands string, maintenance bool) {
	maintenance = true
	srv := d.serverNode(server)
	if srv == nil {
		return "", "", true
	}
	apps, _ := lookupKey(srv, "apps")
	if apps == nil {
		return "", "", true
	}
	appNode, _ := lookupKey(apps, app)
	if appNode == nil {
		return "", "", true
	}
	if build, _ := lookupKey(appNode, "build"); build != nil {
		if c, _ := lookupKey(build, "commands"); c != nil {
			buildCommands = strings.TrimRight(c.Value, "\n")
		}
	}
	if deploy, _ := lookupKey(appNode, "deploy"); deploy != nil {
		if c, _ := lookupKey(deploy, "commands"); c != nil {
			deployCommands = strings.TrimRight(c.Value, "\n")
		}
		if m, _ := lookupKey(deploy, "maintenance"); m != nil {
			if parsed, err := strconv.ParseBool(strings.TrimSpace(m.Value)); err == nil {
				maintenance = parsed
			}
		}
	}
	return buildCommands, deployCommands, maintenance
}

// applyIdentityFile writes identity_file. The form shows the expanded path, so
// an unchanged value keeps what the user wrote (for example ~/.ssh/prod).
func applyIdentityFile(srv *yaml.Node, value string) {
	if value == "" {
		removeKey(srv, "identity_file")
		return
	}
	if existing, _ := lookupKey(srv, "identity_file"); existing != nil && expandTilde(existing.Value) == expandTilde(value) {
		return
	}
	setString(srv, "identity_file", value)
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
}
