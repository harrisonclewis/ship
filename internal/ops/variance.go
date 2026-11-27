package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/harrisonclewis/ship/internal/config"
)

// Config variance states reported for an app.
const (
	VarianceApplied     = "applied"
	VariancePending     = "pending deploy"
	VarianceNotDeployed = "not deployed"
)

// AppVariance is one app's desired-vs-running state.
type AppVariance struct {
	App               string
	State             string
	RecordChanged     bool
	ComposeChanged    bool
	DockerfileChanged bool
	Diff              string
}

// VarianceReport is the full desired-vs-running state of a server.
type VarianceReport struct {
	Server         string
	Apps           []AppVariance
	ProxyOutOfSync bool
	ProxyReason    string
	// ProxyDiff is the active -> rendered Caddyfile diff when they differ.
	ProxyDiff    string
	SetupPending bool
	SetupReason  string
	State        *ServerConfig
}

// Variances fetches the server configuration and compares desired with running.
func (o *Ops) Variances(ctx context.Context, server string) (VarianceReport, error) {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return VarianceReport{}, err
	}
	return ComputeVariances(state), nil
}

// ComputeVariances compares desired and running server configuration.
func ComputeVariances(state *ServerConfig) VarianceReport {
	report := VarianceReport{Server: state.Server, State: state}
	for _, name := range state.AppNames() {
		da := state.Apps[name]
		ra := state.Running[name]
		v := AppVariance{App: name}
		deployed := ra != nil && (ra.Record != nil || len(ra.Meta) > 0)
		switch {
		case !deployed:
			v.State = VarianceNotDeployed
		case da.Record == nil || ra.Record == nil:
			v.State = VariancePending
			v.RecordChanged = true
		default:
			v.RecordChanged = !recordsEqualIgnoringRoutes(da.Record, ra.Record)
			v.ComposeChanged = !bytes.Equal(da.Compose, ra.Compose)
			v.DockerfileChanged = !dockerfilesEqual(da, ra)
			if v.RecordChanged || v.ComposeChanged || v.DockerfileChanged {
				v.State = VariancePending
			} else {
				v.State = VarianceApplied
			}
		}
		v.Diff = appVarianceDiff(da, ra)
		report.Apps = append(report.Apps, v)
	}

	// Comment lines (the generated header) don't change routing, so a header
	// reworded by a newer ship doesn't count as out of sync.
	rendered := state.CaddyfileFor()
	renderedHash := sha256.Sum256(caddyfileWithoutComments([]byte(rendered)))
	activeHash := sha256.Sum256(caddyfileWithoutComments(state.ActiveCaddyfile))
	if hex.EncodeToString(renderedHash[:]) != hex.EncodeToString(activeHash[:]) {
		report.ProxyOutOfSync = true
		report.ProxyReason = "the Caddyfile that would be rendered differs from the active one"
		report.ProxyDiff = UnifiedDiff("active/Caddyfile", "rendered/Caddyfile",
			caddyfileWithoutComments(state.ActiveCaddyfile), caddyfileWithoutComments([]byte(rendered)))
	} else if state.RunningImage == "" {
		report.ProxyOutOfSync = true
		report.ProxyReason = "the proxy is not running"
	} else if state.Settings.CaddyImage != state.RunningImage {
		report.ProxyOutOfSync = true
		report.ProxyReason = "the desired caddy_image differs from the running image"
	}

	records := make(map[string]*config.App, len(state.Apps))
	haveRecord := false
	for name, da := range state.Apps {
		if da.Record != nil {
			records[name] = da.Record
			haveRecord = true
		}
	}
	wantApps := AppsSpecFor(records)
	switch {
	case !state.AppliedSetup.Present:
		if haveRecord || state.Settings.Present {
			report.SetupPending = true
			report.SetupReason = "setup has not run since the server became authoritative"
		}
	case state.AppliedSetup.AdminUser != state.Settings.AdminUser:
		report.SetupPending = true
		report.SetupReason = "admin_user changed since the last setup"
	case state.AppliedSetup.Apps != wantApps:
		report.SetupPending = true
		report.SetupReason = "app uids changed since the last setup"
	}
	return report
}

func recordsEqualIgnoringRoutes(a, b *config.App) bool {
	ca, err := comparableRecord(a)
	if err != nil {
		return false
	}
	cb, err := comparableRecord(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ca, cb)
}

func comparableRecord(a *config.App) ([]byte, error) {
	clone := *a
	clone.Domains = nil
	clone.Redirects = nil
	return clone.RecordYAML()
}

func dockerfilesEqual(a *DesiredApp, b *RunningApp) bool {
	if a.HasDockerfile != b.HasDockerfile {
		return false
	}
	return bytes.Equal(a.Dockerfile, b.Dockerfile)
}

// appVarianceDiff shows what a deploy would change: running -> desired.
func appVarianceDiff(da *DesiredApp, ra *RunningApp) string {
	if da == nil || ra == nil {
		return ""
	}
	var out string
	if da.Record != nil && ra.Record != nil {
		desired, err := da.Record.RecordYAML()
		if err == nil {
			running, err := ra.Record.RecordYAML()
			if err == nil {
				out += UnifiedDiff("running/ship-app.yaml", "desired/ship-app.yaml", running, desired)
			}
		}
	}
	out += UnifiedDiff("running/compose.yaml", "desired/compose.yaml", ra.Compose, da.Compose)
	if da.HasDockerfile || ra.HasDockerfile {
		out += UnifiedDiff("running/Dockerfile", "desired/Dockerfile", ra.Dockerfile, da.Dockerfile)
	}
	return out
}

func caddyfileWithoutComments(b []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("#")) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}
