# Development

```sh
make build    # ./bin/ship, version from git describe
make test     # go test ./...
make check    # everything CI runs
make dist     # all six release binaries in dist/
```

`make check` runs `gofmt`, `go vet`, `go test`, `sh -n`/`bash -n` on the scripts, and `shellcheck` when it is installed. Unit tests cover config validation and lookup, Caddyfile rendering (golden file), command construction and dry-run output, TUI models and `ship.yaml` editing, and update signature checks, downloads and installs.

## Layout

| Path | What |
|---|---|
| `main.go`, `internal/cli` | the `ship` CLI |
| `internal/config` | finds, loads and validates `ship.yaml` |
| `internal/remote` | builds and runs `ssh`, `docker` and local commands |
| `internal/render` | renders the Caddyfile |
| `internal/ops` | every server operation, shared by the CLI and the TUI |
| `internal/tui` | the interactive TUI (Bubble Tea) |
| `internal/update`, `internal/version` | self-update and the stamped version |
| `ship-agent` | the server script, embedded in the binary |
| `examples/` | a sample `ship.yaml` and app folders |
| `install.sh`, `install.ps1` | one-line installers |
| `scripts/release-keygen.sh` | creates the release signing key |
| `.github/workflows/` | CI on Linux, macOS and Windows, and the release build |

Your own `ship.yaml` and `apps/` belong in `~/.config/ship`, not in this repo; both are git-ignored here.
