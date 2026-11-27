package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/harrisonclewis/ship/internal/config"
)

// ConfigPush migrates one server from the old full-format local ship.yaml to
// server-side desired configuration, then rewrites that server's local block.
func (o *Ops) ConfigPush(ctx context.Context, server string) error {
	old := o.Config
	srv, err := old.Server(server)
	if err != nil {
		return err
	}
	if old.Path == "" {
		return errors.New("cannot rewrite ship.yaml: no loaded file path")
	}
	if _, err := config.Load(old.Path); err == nil {
		return fmt.Errorf("server %s is already in the new local format; nothing to push", server)
	}
	if !srv.IsOldFormat() {
		return fmt.Errorf("server %s has no app configuration to push", server)
	}

	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return err
	}

	records := make(map[string]*config.App, len(state.Apps)+len(srv.Apps))
	for name, da := range state.Apps {
		if da.Record != nil {
			records[name] = da.Record
		}
	}
	type prepared struct {
		name       string
		record     *config.App
		compose    []byte
		dockerfile []byte
	}
	var apps []prepared
	for _, name := range srv.AppNames() {
		app := srv.Apps[name]
		if app == nil {
			return fmt.Errorf("apps.%s: app definition is empty", name)
		}
		record := *app
		record.Source.Path = ""
		if err := record.Validate(); err != nil {
			return err
		}
		composePath := srv.ComposePath(name)
		compose, err := os.ReadFile(composePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", composePath, err)
		}
		var dockerfile []byte
		if record.Source.Type == "github-release" {
			dockerfilePath := srv.DockerfilePath(name)
			dockerfile, err = os.ReadFile(dockerfilePath)
			if err != nil {
				return fmt.Errorf("read %s: %w", dockerfilePath, err)
			}
		}
		records[name] = &record
		apps = append(apps, prepared{name: name, record: &record, compose: compose, dockerfile: dockerfile})
	}
	if err := config.ValidateRecords(records); err != nil {
		return err
	}

	if _, err := o.PutServerSettings(ctx, server, srv.AdminUser, srv.CaddyImage, state.Settings.Hash); err != nil {
		return err
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].name < apps[j].name })
	for _, a := range apps {
		expected := ""
		if da := state.Apps[a.name]; da != nil {
			expected = da.Hash
		}
		if _, err := o.PutDesiredApp(ctx, server, a.name, a.record, a.compose, a.dockerfile, state.Arch, expected); err != nil {
			return fmt.Errorf("push %s: %w", a.name, err)
		}
		fmt.Fprintf(o.opts.Out, "pushed desired configuration for %s\n", a.name)
	}

	if !state.AppliedSetup.Present {
		if err := o.RecordSetup(ctx, server, srv.AdminUser, AppsSpecFor(records)); err != nil {
			return fmt.Errorf("record setup state: %w", err)
		}
		if !o.opts.DryRun {
			fmt.Fprintf(o.opts.Out, "Recorded setup as already applied for %s.\n", srv.AdminUser)
		}
	}

	if o.opts.DryRun {
		fmt.Fprintf(o.opts.Out, "# would rewrite %s for the new local format (.bak kept)\n", old.Path)
		return nil
	}
	if err := rewriteLocalForServer(old.Path, srv); err != nil {
		return err
	}
	fmt.Fprintf(o.opts.Out, "Rewrote %s for the new local format (.bak kept).\n", old.Path)
	if len(apps) == 0 {
		fmt.Fprintln(o.opts.Out, "No apps were configured locally for this server.")
	}
	return nil
}

// rewriteLocalForServer replaces one server block with the new local shape and
// keeps every other server block untouched.
func rewriteLocalForServer(path string, srv *config.Server) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return errors.New("ship.yaml is not a single YAML document")
	}
	root := doc.Content[0]
	servers, err := mappingValue(root, "servers")
	if err != nil {
		return err
	}
	if servers == nil || servers.Kind != yaml.MappingNode {
		return errors.New("ship.yaml has no servers mapping; cannot migrate")
	}
	var target *yaml.Node
	for i := 0; i+1 < len(servers.Content); i += 2 {
		if servers.Content[i].Value == srv.Name {
			target = servers.Content[i+1]
			break
		}
	}
	if target == nil || target.Kind != yaml.MappingNode {
		return fmt.Errorf("servers.%s is not a mapping", srv.Name)
	}

	// Keep paths exactly as written (e.g. ~/code/web), not as resolved.
	rawSources := map[string]string{}
	if apps, _ := mappingValue(target, "apps"); apps != nil && apps.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(apps.Content); i += 2 {
			source, _ := mappingValue(apps.Content[i+1], "source")
			if source == nil {
				continue
			}
			if p, _ := mappingValue(source, "path"); p != nil && p.Kind == yaml.ScalarNode && p.Value != "" {
				rawSources[apps.Content[i].Value] = p.Value
			}
		}
	}
	deleteMapping(target, "admin_user")
	deleteMapping(target, "arch")
	deleteMapping(target, "caddy_image")
	deleteMapping(target, "apps")

	sources := map[string]string{}
	for name, path := range srv.Sources {
		sources[name] = path
	}
	for name, app := range srv.Apps {
		if app != nil && app.Source.Type == "image" && app.Source.Path != "" {
			sources[name] = app.Source.Path
			if raw := rawSources[name]; raw != "" {
				sources[name] = raw
			}
		}
	}
	if len(sources) > 0 {
		names := make([]string, 0, len(sources))
		for name := range sources {
			names = append(names, name)
		}
		sort.Strings(names)
		node := &yaml.Node{Kind: yaml.MappingNode}
		for _, name := range names {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: name},
				&yaml.Node{Kind: yaml.ScalarNode, Value: sources[name]},
			)
		}
		setMappingNode(target, "sources", node)
	} else {
		deleteMapping(target, "sources")
	}

	buf, err := config.MarshalYAMLNode(&doc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".bak", original, 0o600); err != nil {
		return fmt.Errorf("write %s.bak: %w", path, err)
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path, buf, mode); err != nil {
		return err
	}
	return nil
}

func mappingValue(root *yaml.Node, key string) (*yaml.Node, error) {
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("ship.yaml root is not a mapping")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1], nil
		}
	}
	return nil, nil
}

func setMapping(root *yaml.Node, key, value string) {
	setMappingNode(root, key, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

func setMappingNode(root *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = value
			return
		}
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		value,
	)
}

func deleteMapping(root *yaml.Node, key string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}
