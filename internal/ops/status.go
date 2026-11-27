package ops

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// AppStatus is one app's machine-readable state.
type AppStatus struct {
	App         string
	Current     string
	Previous    string
	Health      string
	Maintenance bool
	Config      string
}

// StatusReport is a server's machine-readable state.
type StatusReport struct {
	Server         string
	Proxy          string
	RebootRequired bool
	Apps           []AppStatus
	ProxyOutOfSync bool
	SetupPending   bool
}

// Status fetches machine-readable state for one app (or every desired app when
// app is empty), parses it and writes a short human summary. The TUI can use
// the returned report and ignore the output.
func (o *Ops) Status(ctx context.Context, server, app string) (StatusReport, error) {
	variance, err := o.Variances(ctx, server)
	if err != nil {
		return StatusReport{}, err
	}
	state := variance.State
	if app != "" {
		if state.Apps[app] == nil {
			return StatusReport{}, fmt.Errorf("app %q is not configured on %s", app, server)
		}
	}
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return StatusReport{}, err
	}
	if err := o.ensureAgent(s); err != nil {
		return StatusReport{}, err
	}
	args := []string{"status", "--format=kv"}
	if app != "" {
		args = append(args, app)
	}
	out, err := s.OutputAgent(args...)
	if err != nil {
		return StatusReport{}, err
	}
	report, err := ParseStatusKV(out)
	report.Server = srv.Name
	report.ProxyOutOfSync = variance.ProxyOutOfSync
	report.SetupPending = variance.SetupPending
	if err != nil {
		return report, err
	}
	byApp := map[string]AppVariance{}
	for _, v := range variance.Apps {
		byApp[v.App] = v
	}
	seen := map[string]bool{}
	for i := range report.Apps {
		a := &report.Apps[i]
		seen[a.App] = true
		if v, ok := byApp[a.App]; ok {
			a.Config = v.State
		}
	}
	for _, v := range variance.Apps {
		if seen[v.App] || (app != "" && v.App != app) {
			continue
		}
		report.Apps = append(report.Apps, AppStatus{App: v.App, Config: v.State, Health: "missing"})
	}
	sort.Slice(report.Apps, func(i, j int) bool { return report.Apps[i].App < report.Apps[j].App })
	o.printStatus(report)
	return report, nil
}

// ParseStatusKV parses `ship-agent status --format=kv` output. Each record is
// one line: a `server` line, then one `app` line per app.
func ParseStatusKV(out []byte) (StatusReport, error) {
	var report StatusReport
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		kv := map[string]string{}
		for _, f := range fields[1:] {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				return report, fmt.Errorf("invalid status field %q", f)
			}
			kv[k] = v
		}
		switch fields[0] {
		case "server":
			report.Proxy = dashValue(kv["proxy"])
			report.RebootRequired = kv["reboot_required"] == "yes"
		case "app":
			name := kv["name"]
			if name == "" {
				return report, fmt.Errorf("status app record has no name: %q", line)
			}
			report.Apps = append(report.Apps, AppStatus{
				App:         name,
				Current:     dashValue(kv["current"]),
				Previous:    dashValue(kv["previous"]),
				Health:      dashValue(kv["health"]),
				Maintenance: kv["maintenance"] == "on",
			})
		default:
			return report, fmt.Errorf("unexpected status record %q", fields[0])
		}
	}
	return report, nil
}

func dashValue(v string) string {
	if v == "-" {
		return ""
	}
	return v
}

func displayValue(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func (o *Ops) printStatus(report StatusReport) {
	if len(report.Apps) == 0 && report.Proxy == "" && !report.RebootRequired {
		return
	}
	for _, a := range report.Apps {
		fmt.Fprintf(o.opts.Out, "%-20s current=%s previous=%s health=%s config=%s",
			a.App, displayValue(a.Current), displayValue(a.Previous), displayValue(a.Health), displayValue(a.Config))
		if a.Maintenance {
			fmt.Fprint(o.opts.Out, " maintenance=on")
		}
		fmt.Fprintln(o.opts.Out)
	}
	fmt.Fprintf(o.opts.Out, "proxy: %s\n", displayValue(report.Proxy))
	server := report.Server
	if server == "" {
		server = "<server>"
	}
	if report.ProxyOutOfSync {
		fmt.Fprintf(o.opts.Out, "proxy config out of sync; run `ship %s proxy restart` to apply it\n", server)
	}
	if report.SetupPending {
		fmt.Fprintf(o.opts.Out, "setup pending; run `ship %s setup` when convenient\n", server)
	}
	if report.RebootRequired {
		fmt.Fprintln(o.opts.Out, "reboot required")
	}
}
