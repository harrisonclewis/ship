package yamldoc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// AppYAMLHeader renders the comment shown above an app being edited on its own.
func AppYAMLHeader(server, app string) string {
	return fmt.Sprintf("# ship.yaml: server %s, app %s\n# Edit only this app's settings; the app name and server are fixed.\n\n", server, app)
}

// AppYAML renders one app's mapping as a standalone YAML document, comments
// included, with a short header naming the server and app.
func (d *Doc) AppYAML(server, app string) ([]byte, error) {
	node, err := d.appMapping(server, app)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(buf.Bytes())+len(AppYAMLHeader(server, app)))
	out = append(out, AppYAMLHeader(server, app)...)
	out = append(out, buf.Bytes()...)
	return out, nil
}

// ReplaceAppYAML parses data as one app mapping and splices it back in place
// of the app's current mapping. The app key and the rest of the document are
// left alone.
func (d *Doc) ReplaceAppYAML(server, app string, data []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("app YAML is empty")
		}
		return err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("app YAML must contain exactly one YAML document")
		}
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("app YAML must be a mapping")
	}

	apps, err := d.appsMap(server)
	if err != nil {
		return err
	}
	_, index := lookupKey(apps, app)
	if index < 0 {
		return fmt.Errorf("app %q is no longer in ship.yaml", app)
	}
	old := apps.Content[index+1]
	next := doc.Content[0]
	if next.HeadComment == "" {
		next.HeadComment = old.HeadComment
	}
	if next.LineComment == "" {
		next.LineComment = old.LineComment
	}
	if next.FootComment == "" {
		next.FootComment = old.FootComment
	}
	apps.Content[index+1] = next
	return nil
}

func (d *Doc) appMapping(server, app string) (*yaml.Node, error) {
	apps, err := d.appsMap(server)
	if err != nil {
		return nil, err
	}
	_, index := lookupKey(apps, app)
	if index < 0 {
		return nil, fmt.Errorf("app %q is no longer in ship.yaml", app)
	}
	node := apps.Content[index+1]
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("app %q is not a YAML mapping", app)
	}
	return node, nil
}

// SetAppSourcePath updates source.path. An empty value removes the key.
func (d *Doc) SetAppSourcePath(server, app, value string) error {
	return d.updateAppField(server, app, "source", "path", value)
}

// SetAppSourceRepo updates source.repo. An empty value removes the key.
func (d *Doc) SetAppSourceRepo(server, app, value string) error {
	return d.updateAppField(server, app, "source", "repo", value)
}

// SetAppSourceAsset updates source.asset. An empty value removes the key.
func (d *Doc) SetAppSourceAsset(server, app, value string) error {
	return d.updateAppField(server, app, "source", "asset", value)
}

func (d *Doc) updateAppField(server, app, parent, key, value string) error {
	node, err := d.appMapping(server, app)
	if err != nil {
		return err
	}
	if value == "" {
		if m, _ := lookupKey(node, parent); m != nil {
			removeKey(m, key)
		}
		return nil
	}
	m := ensureMap(node, parent)
	setString(m, key, value)
	return nil
}

// SetAppDomains replaces the app's domain list.
func (d *Doc) SetAppDomains(server, app string, values []string) error {
	return d.updateAppList(server, app, "domains", values)
}

// SetAppRedirects replaces the app's redirect list.
func (d *Doc) SetAppRedirects(server, app string, values []string) error {
	return d.updateAppList(server, app, "redirects", values)
}

func (d *Doc) updateAppList(server, app, key string, values []string) error {
	node, err := d.appMapping(server, app)
	if err != nil {
		return err
	}
	setStringSeq(node, key, values)
	return nil
}

// SetAppBuildCommands updates build.commands. Empty text removes the build map.
func (d *Doc) SetAppBuildCommands(server, app, value string) error {
	node, err := d.appMapping(server, app)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		removeKey(node, "build")
		return nil
	}
	build := ensureMap(node, "build")
	setLiteral(build, "commands", value)
	return nil
}

// SetAppDeployCommands updates deploy.commands. Empty text removes the command
// and the deploy map when nothing else is left in it.
func (d *Doc) SetAppDeployCommands(server, app, value string) error {
	node, err := d.appMapping(server, app)
	if err != nil {
		return err
	}
	deploy := ensureMap(node, "deploy")
	if strings.TrimSpace(value) == "" {
		removeKey(deploy, "commands")
	} else {
		setLiteral(deploy, "commands", value)
	}
	removeEmptyMap(node, "deploy")
	return nil
}

// SetAppDeployMaintenance writes deploy.maintenance. A nil value removes the
// key so the default applies.
func (d *Doc) SetAppDeployMaintenance(server, app string, value *bool) error {
	node, err := d.appMapping(server, app)
	if err != nil {
		return err
	}
	if value == nil {
		if deploy, _ := lookupKey(node, "deploy"); deploy != nil {
			removeKey(deploy, "maintenance")
		}
		removeEmptyMap(node, "deploy")
		return nil
	}
	deploy := ensureMap(node, "deploy")
	setBool(deploy, "maintenance", *value)
	return nil
}

func removeEmptyMap(m *yaml.Node, key string) {
	if v, _ := lookupKey(m, key); isEmptyMapping(v) {
		removeKey(m, key)
	}
}
