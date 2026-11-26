// Package tui is the full-screen interactive ship interface.
package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/harrisonclewis/ship/internal/config"
)

// Options configures the interface.
type Options struct {
	DryRun bool
	Agent  []byte
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// Run opens the full-screen interface. A missing ship.yaml starts an empty
// interface; saving creates it in the user's config directory.
func Run(opts Options) error {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.ErrOut
	if errOut == nil {
		errOut = os.Stderr
	}

	cfg, path, doc, err := loadForTUI(errOut)
	if err != nil {
		return err
	}
	backend := newRealBackend(cfg, opts.DryRun, opts.Agent, in)
	m := newModel(cfg, backend, doc, path, opts.DryRun)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	_, err = p.Run()
	return err
}

func loadForTUI(errOut io.Writer) (*config.Config, string, *yamlDoc, error) {
	path, err := config.Find()
	if err == nil {
		cfg, loadErr := config.Load(path)
		if loadErr != nil {
			return nil, "", nil, loadErr
		}
		doc, docErr := loadYAMLDoc(path)
		if docErr != nil {
			return nil, "", nil, docErr
		}
		return cfg, path, doc, nil
	}

	// No ship.yaml yet. Start empty; the first save creates the file.
	path = os.Getenv("SHIP_CONFIG")
	if path == "" {
		path, err = config.UserConfigPath()
		if err != nil {
			return nil, "", nil, err
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", nil, err
	}
	cfg := &config.Config{BackupDirLocal: config.DefaultBackupDir, Dir: filepath.Dir(abs)}
	if err := cfg.Validate(); err != nil {
		return nil, "", nil, err
	}
	if errOut != nil {
		fmt.Fprintf(errOut, "ship: %s not found; starting with an empty configuration\n", path)
	}
	return cfg, abs, newEmptyYAMLDoc(abs), nil
}
