package ops

import (
	"context"
	"fmt"
	"strings"
)

// AppStatus is one app's machine-readable state.
type AppStatus struct {
	App         string
	Current     string
	Previous    string
	Health      string
	Maintenance bool
}

// StatusReport is a server's machine-readable state.
type StatusReport struct {
	Server         string
	Proxy          string
	RebootRequired bool
	Apps           []AppStatus
}

// Status fetches machine-readable state for one app (or every app when app is
// empty), parses it and writes a short human summary. The TUI can use the
// returned report and ignore the output.
func (o *Ops) Status(ctx context.Context, server, app string) (StatusReport, error) {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return StatusReport{}, err
	}
	if app != "" {
		if _, err := srv.App(app); err != nil {
			return StatusReport{}, err
		}
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
	if err != nil {
		return report, err
	}
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
		fmt.Fprintf(o.opts.Out, "%-20s current=%s previous=%s health=%s",
			a.App, displayValue(a.Current), displayValue(a.Previous), displayValue(a.Health))
		if a.Maintenance {
			fmt.Fprint(o.opts.Out, " maintenance=on")
		}
		fmt.Fprintln(o.opts.Out)
	}
	fmt.Fprintf(o.opts.Out, "proxy: %s\n", displayValue(report.Proxy))
	if report.RebootRequired {
		fmt.Fprintln(o.opts.Out, "reboot required")
	}
}
