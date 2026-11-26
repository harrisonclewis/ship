<h1 align="center">ship</h1>

<p align="center">Deploy and run your apps on your own servers from your terminal.</p>

---

### What it is

`ship` is one Go binary that drives your servers over your system `ssh`. On the server it installs a single root-run script, `ship-agent`, and every action is `sudo ship-agent <action>`.

- **Caddy in front.** Apps sit on a private Docker network; only Caddy publishes ports, and it gets certificates for you.
- **Several servers.** One `ship.yaml` describes all your servers and the apps on each.
- **Safe switches.** Every deploy shows a maintenance page, takes a backup, runs your deploy commands, waits for the healthcheck and rolls back on failure.
- **Two app types.** Build an image from a GitHub release binary on the server, or build one from a local repo and send it over SSH. No registry.
- **Backups.** Nightly and on demand, for SQLite, files and Postgres, with checksum-verified downloads and restore.

### Installation

```bash
# Linux and macOS
curl -fsSL https://raw.githubusercontent.com/harrisonclewis/ship/main/install.sh | bash

# Windows (PowerShell)
irm https://raw.githubusercontent.com/harrisonclewis/ship/main/install.ps1 | iex
```

Binaries for Linux, macOS and Windows (x64 and arm64) are on the [releases page](https://github.com/harrisonclewis/ship/releases). Run `ship update` to update. You also need OpenSSH, and Docker with buildx for image apps.

### Usage

```sh
ship
```

Opens the TUI: a **Servers** tab listing each server with its IP and apps, and an **Activity** tab with every action's output. Select a server to deploy, back up, roll back or restart its apps, deploy them all at once, or add and edit servers and apps.

Every action is also a command, for scripts and CI:

```sh
ship prod setup               # prepare the server
ship prod deploy hello        # deploy one app
ship prod deploy all          # deploy every app on prod
ship prod status
```

Add `--dry-run` to see what would run without touching a server.

### Documentation

- [Install](docs/install.md)
- [Updates](docs/updates.md)
- [TUI](docs/tui.md)
- [Server setup](docs/setup.md)
- [Configuration](docs/configuration.md)
- [Deploying](docs/deploying.md)
- [Adding an app](docs/apps.md)
- [Backups and restore](docs/backups.md)
- [Commands](docs/commands.md)
- [Security](docs/security.md)
- [Releasing](docs/releasing.md)

### Contributing

```sh
make check        # gofmt, go vet, go test, bash -n and shellcheck
```

See [Development](docs/development.md) for the code layout.
