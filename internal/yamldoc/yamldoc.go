package yamldoc

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
	"github.com/harrisonclewis/ship/internal/localedit"
)

// ErrConfigChanged means ship.yaml no longer matches what this document
// loaded.
var ErrConfigChanged = localedit.ErrChanged

// ServerValues are the fields the server form edits.
type ServerValues struct {
	Name         string
	Host         string
	IdentityFile string
	AdminUser    string
	Arch         string
	CaddyImage   string
	OriginalName string
	IsNew        bool
}

// AppValues are the fields the app form edits. The raw text fields are kept
// alongside the parsed slices so the TUI can show what the user typed.
type AppValues struct {
	Server            string
	Name              string
	OriginalName      string
	IsNew             bool
	DomainsText       string
	RedirectsText     string
	Upstream          string
	SourceType        string
	SourceRepo        string
	SourceAsset       string
	SourcePath        string
	UID               string
	DataText          string
	HealthPath        string
	BackupSQLite      string
	BackupFiles       string
	PostgresService   string
	PostgresDB        string
	PostgresUser      string
	Env               string
	BuildCommands     string
	DeployCommands    string
	DeployMaintenance bool

	Domains       []string
	Redirects     []string
	Data          []string
	BackupSQLiteV []string
	BackupFilesV  []string
}

// Doc is one ship.yaml document being edited in place.
type Doc struct {
	path     string
	original []byte
	exists   bool
	mode     os.FileMode
	root     yaml.Node
}

func Load(path string) (*Doc, error) {
	d := &Doc{path: path, mode: 0o600}
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

func New(path string) *Doc {
	return &Doc{path: path, mode: 0o600}
}

func (d *Doc) mapping() *yaml.Node {
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

func (d *Doc) Clone() *Doc {
	out := &Doc{
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

func (d *Doc) bytes() ([]byte, error) {
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

// Validate renders the document and checks it with the config package without
// writing anything.
func (d *Doc) Validate() error {
	rendered, err := d.bytes()
	if err != nil {
		return err
	}
	_, err = config.Decode(bytes.NewReader(rendered))
	return err
}

// Save validates the whole document, checks that the file on disk is still the
// version this document loaded, then replaces it atomically and keeps a .bak
// copy.
func (d *Doc) Save() error {
	rendered, err := d.bytes()
	if err != nil {
		return err
	}
	if _, err := config.Decode(bytes.NewReader(rendered)); err != nil {
		return err
	}

	if _, err := localedit.ReplaceFile(d.path, d.original, rendered, d.exists, d.mode); err != nil {
		return err
	}
	d.original = rendered
	d.exists = true
	if d.mode == 0 {
		d.mode = 0o600
	}
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

func (d *Doc) serversMap() *yaml.Node {
	return ensureMap(d.mapping(), "servers")
}

func (d *Doc) serverNode(name string) *yaml.Node {
	v, _ := lookupKey(d.serversMap(), name)
	return v
}

func (d *Doc) ApplyServer(v ServerValues) error {
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

func (d *Doc) DeleteServer(name string) error {
	servers := d.serversMap()
	_, index := lookupKey(servers, name)
	if index < 0 {
		return fmt.Errorf("server %q is no longer in ship.yaml", name)
	}
	servers.Content = append(servers.Content[:index], servers.Content[index+2:]...)
	return nil
}

func (d *Doc) appsMap(server string) (*yaml.Node, error) {
	srv := d.serverNode(server)
	if srv == nil {
		return nil, fmt.Errorf("server %q is no longer in ship.yaml", server)
	}
	return ensureMap(srv, "apps"), nil
}

func (d *Doc) ApplyApp(v AppValues) error {
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

func applyBackup(app *yaml.Node, v AppValues) {
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

func applyDeploy(app *yaml.Node, v AppValues) {
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

// PutApp adds or replaces an app from a YAML record, which must already have
// been validated by the config package. On replace it merges the record into
// the existing app node, so local comments and styles survive where values are
// unchanged. Keys the record does not have are removed. When keepPath is set
// and the old app has a source.path, that value is carried over (imports must
// not lose the local build directory).
func (d *Doc) PutApp(server, name string, record []byte, keepPath bool) error {
	apps, err := d.appsMap(server)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(record, &doc); err != nil {
		return fmt.Errorf("app %q record: %w", name, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("app %q record is not a YAML mapping", name)
	}
	rec := doc.Content[0]

	oldPath := ""
	_, oldIndex := lookupKey(apps, name)
	if oldIndex >= 0 {
		old := apps.Content[oldIndex+1]
		if source, _ := lookupKey(old, "source"); source != nil {
			if p, _ := lookupKey(source, "path"); p != nil {
				oldPath = p.Value
			}
		}
		if keepPath && oldPath != "" {
			// Put the local path into the record before merging, so the
			// existing path node (and its comment and style) is kept.
			if source, _ := lookupKey(rec, "source"); source != nil {
				if typ, _ := lookupKey(source, "type"); typ != nil && typ.Value == "image" {
					setString(source, "path", oldPath)
				}
			}
		}
		mergeNode(old, rec)
	} else {
		appendKey(apps, name, rec)
	}
	return nil
}

// mergeNode updates dst from src while keeping dst's comments and style. A
// value that is already equal is left completely untouched. Mappings are
// merged key by key; every other differing value is replaced in place.
func mergeNode(dst, src *yaml.Node) {
	if dst == nil || src == nil || nodeEqual(dst, src) {
		return
	}
	if dst.Kind == yaml.MappingNode && src.Kind == yaml.MappingNode {
		mergeMapping(dst, src)
		return
	}
	replaceNodeValue(dst, src)
}

func mergeMapping(dst, src *yaml.Node) {
	srcByKey := make(map[string]*yaml.Node, len(src.Content)/2)
	for i := 0; i+1 < len(src.Content); i += 2 {
		srcByKey[src.Content[i].Value] = src.Content[i+1]
	}
	used := make(map[string]bool, len(srcByKey))
	out := make([]*yaml.Node, 0, len(src.Content))
	for i := 0; i+1 < len(dst.Content); i += 2 {
		key, value := dst.Content[i], dst.Content[i+1]
		srcValue, ok := srcByKey[key.Value]
		if !ok || used[key.Value] {
			continue
		}
		mergeNode(value, srcValue)
		out = append(out, key, value)
		used[key.Value] = true
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		key, value := src.Content[i], src.Content[i+1]
		if used[key.Value] {
			continue
		}
		out = append(out, key, value)
		used[key.Value] = true
	}
	dst.Content = out
}

func replaceNodeValue(dst, src *yaml.Node) {
	style := dst.Style
	head, line, foot := dst.HeadComment, dst.LineComment, dst.FootComment
	*dst = *src
	if style != 0 {
		dst.Style = style
	}
	if head != "" {
		dst.HeadComment = head
	}
	if line != "" {
		dst.LineComment = line
	}
	if foot != "" {
		dst.FootComment = foot
	}
}

// nodeEqual compares YAML data, ignoring comments, styles and mapping order.
func nodeEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Tag != b.Tag {
		return false
	}
	switch a.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !nodeEqual(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	case yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		values := make(map[string]*yaml.Node, len(b.Content)/2)
		for i := 0; i+1 < len(b.Content); i += 2 {
			values[b.Content[i].Value] = b.Content[i+1]
		}
		for i := 0; i+1 < len(a.Content); i += 2 {
			value, ok := values[a.Content[i].Value]
			if !ok || !nodeEqual(a.Content[i+1], value) {
				return false
			}
		}
		return true
	default:
		return a.Value == b.Value
	}
}

func (d *Doc) DeleteApp(server, app string) error {
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
func (d *Doc) AppSourceRaw(server, app string) (asset, sourcePath string) {
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
func (d *Doc) AppExtras(server, app string) (buildCommands, deployCommands string, maintenance bool) {
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
