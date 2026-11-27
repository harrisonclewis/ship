package ops

import (
	"fmt"
	"path/filepath"

	"github.com/harrisonclewis/ship/internal/config"
)

// BuildApprovalError reports a build script read from the server that this
// machine has not approved. Callers that can prompt should show Script and then
// call RecordBuildApproval (or set Options.ApproveBuild).
type BuildApprovalError struct {
	Server string
	App    string
	Script string
	Hash   string
}

func (e *BuildApprovalError) Error() string {
	return fmt.Sprintf("the build script for %s on %s has not been approved on this machine; run the deploy again with --yes after reviewing it, or approve it when prompted", e.App, e.Server)
}

// RecordBuildApproval remembers that this machine's user approved the current
// build script for an app.
func (o *Ops) RecordBuildApproval(server, app, script string) error {
	return config.RecordBuildApproval(o.configDir(), server, app, config.HashBuildCommands(script))
}

func (o *Ops) configDir() string {
	if o.Config.Dir != "" {
		return o.Config.Dir
	}
	if o.Config.Path != "" {
		return filepath.Dir(o.Config.Path)
	}
	return "."
}

func (o *Ops) ensureBuildApproval(server, app, script string) error {
	// A dry run only prints the script; it does not execute anything.
	if o.opts.DryRun {
		fmt.Fprintln(o.opts.Out, "# build script (server-provided; not run in dry-run mode):")
		fmt.Fprintln(o.opts.Out, script)
		return nil
	}
	hash := config.HashBuildCommands(script)
	approved, err := config.ApprovedBuildHash(o.configDir(), server, app)
	if err != nil {
		return fmt.Errorf("read build approvals: %w", err)
	}
	if approved == hash {
		return nil
	}
	approvalErr := &BuildApprovalError{Server: server, App: app, Script: script, Hash: hash}
	if o.opts.ApproveBuild == nil {
		return approvalErr
	}
	if err := o.opts.ApproveBuild(approvalErr); err != nil {
		return err
	}
	return o.RecordBuildApproval(server, app, script)
}
