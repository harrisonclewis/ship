package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

// SetLocalSource updates servers.<server>.sources.<app> in a new-format
// ship.yaml, keeping the rest of the file (and its comments) intact. Any
// build_dirs entry for the app is cleared too.
func SetLocalSource(path, server, app, value string, remove bool) error {
	return SetLocalSourceEntry(path, server, app, value, "", remove)
}

// SetLocalSourceEntry writes servers.<server>.sources.<app> and its
// build_dirs.<app> entry in one file update. An empty buildDir removes the
// entry. remove deletes both and the containing maps when they become empty.
func SetLocalSourceEntry(path, server, app, value, buildDir string, remove bool) error {
	dir, err := NormalizeBuildDir(buildDir)
	if err != nil {
		return err
	}
	return editLocalServer(path, server, func(target *yaml.Node) error {
		sources := mapGet(target, "sources")
		if sources == nil || sources.Kind != yaml.MappingNode {
			sources = &yaml.Node{Kind: yaml.MappingNode}
			mapSet(target, "sources", sources)
		}
		if remove {
			mapDelete(sources, app)
			if len(sources.Content) == 0 {
				mapDelete(target, "sources")
			}
			deleteLocalBuildDir(target, app)
			return nil
		}
		mapSet(sources, app, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
		if dir == "" {
			deleteLocalBuildDir(target, app)
			return nil
		}
		setLocalBuildDir(target, app, dir)
		return nil
	})
}

// SetLocalBuildDir updates only servers.<server>.build_dirs.<app>. It leaves
// the source value exactly as written, including a ~/ path.
func SetLocalBuildDir(path, server, app, value string) error {
	dir, err := NormalizeBuildDir(value)
	if err != nil {
		return err
	}
	return editLocalServer(path, server, func(target *yaml.Node) error {
		if dir == "" {
			deleteLocalBuildDir(target, app)
			return nil
		}
		setLocalBuildDir(target, app, dir)
		return nil
	})
}

func setLocalBuildDir(target *yaml.Node, app, dir string) {
	buildDirs := mapGet(target, "build_dirs")
	if buildDirs == nil || buildDirs.Kind != yaml.MappingNode {
		buildDirs = &yaml.Node{Kind: yaml.MappingNode}
		mapSet(target, "build_dirs", buildDirs)
	}
	mapSet(buildDirs, app, &yaml.Node{Kind: yaml.ScalarNode, Value: dir})
}

func deleteLocalBuildDir(target *yaml.Node, app string) {
	buildDirs := mapGet(target, "build_dirs")
	if buildDirs == nil || buildDirs.Kind != yaml.MappingNode {
		return
	}
	mapDelete(buildDirs, app)
	if len(buildDirs.Content) == 0 {
		mapDelete(target, "build_dirs")
	}
}

// editLocalServer loads a new-format ship.yaml, runs edit on the server node
// and writes the file back once.
func editLocalServer(path, server string, edit func(*yaml.Node) error) error {
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
	if root.Kind != yaml.MappingNode {
		return errors.New("ship.yaml root is not a mapping")
	}
	servers := mapGet(root, "servers")
	if servers == nil || servers.Kind != yaml.MappingNode {
		return errors.New("ship.yaml has no servers mapping")
	}
	target := mapGet(servers, server)
	if target == nil || target.Kind != yaml.MappingNode {
		return fmt.Errorf("servers.%s is not a mapping", server)
	}
	if err := edit(target); err != nil {
		return err
	}
	out, err := MarshalYAMLNode(&doc)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path+".bak", original, mode); err != nil {
		return fmt.Errorf("write %s.bak: %w", path, err)
	}
	tmp, err := os.CreateTemp(dirOf(path), ".ship.yaml.tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// MarshalYAMLNode encodes a document with the two-space indent ship.yaml uses.
func MarshalYAMLNode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

func mapGet(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mapSet(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

func mapDelete(n *yaml.Node, key string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}
