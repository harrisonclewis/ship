package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// BuildApprovalFile is the name of the per-machine record of build scripts the
// user has approved. It lives next to ship.yaml, never in it.
const BuildApprovalFile = "build-approvals.yaml"

type buildApprovalDoc struct {
	Servers map[string]map[string]string `yaml:"servers"`
}

// BuildApprovalPath returns the approval state file for a ship.yaml directory.
func BuildApprovalPath(configDir string) string {
	return filepath.Join(configDir, BuildApprovalFile)
}

// HashBuildCommands returns the stable hash stored for an approved script.
func HashBuildCommands(commands string) string {
	sum := sha256.Sum256([]byte(commands))
	return hex.EncodeToString(sum[:])
}

func loadBuildApprovals(path string) (buildApprovalDoc, error) {
	var doc buildApprovalDoc
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// ApprovedBuildHash returns the hash this machine last approved for a server's
// app, or "" when it has approved nothing yet.
func ApprovedBuildHash(configDir, server, app string) (string, error) {
	doc, err := loadBuildApprovals(BuildApprovalPath(configDir))
	if err != nil {
		return "", err
	}
	return doc.Servers[server][app], nil
}

// RecordBuildApproval stores the hash of a build script the user approved.
func RecordBuildApproval(configDir, server, app, hash string) error {
	path := BuildApprovalPath(configDir)
	doc, err := loadBuildApprovals(path)
	if err != nil {
		return err
	}
	if doc.Servers == nil {
		doc.Servers = map[string]map[string]string{}
	}
	if doc.Servers[server] == nil {
		doc.Servers[server] = map[string]string{}
	}
	doc.Servers[server][app] = hash
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".build-approvals-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if err := f.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := f.Write(data); err != nil {
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
	return os.Rename(tmp, path)
}
