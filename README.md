<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="Ship" width="256" height="96">
  </picture>
</p>

<p align="center">Deploy and run your apps on your own servers from your terminal.</p>

---

Point `ship` at a server you own and it runs your apps there, each on its own domain with HTTPS, from one command or a terminal UI. Deploys are safe by default: visitors see a maintenance page while an app updates, and a failed deploy rolls back by itself. Backups, restores and rollbacks are one command each.

### Installation

```bash
# Linux and macOS
curl -fsSL https://raw.githubusercontent.com/harrisonclewis/ship/main/install.sh | bash

# Windows (PowerShell)
irm https://raw.githubusercontent.com/harrisonclewis/ship/main/install.ps1 | iex
```

Binaries for Linux, macOS and Windows (x64 and arm64) are on the [releases page](https://github.com/harrisonclewis/ship/releases). Run `ship update` to update. You also need OpenSSH, and Docker with buildx for image apps.

### Quick start

```sh
git clone https://github.com/docker/welcome-to-docker ~/src/welcome-to-docker
# create ~/.config/ship/ship.yaml with prod: host: root@<ip>
ship prod setup
ship prod lock-ssh                      # after changing host to ops@<ip>
ship prod app create welcome
ship prod app set welcome source.path ~/src/welcome-to-docker
ship prod config edit welcome compose
ship prod deploy welcome
ship prod proxy sync
```

App records, compose files and Dockerfiles live on the server. Create them with `ship <server> app create` and `ship <server> config edit`, or migrate an old local file with `ship <server> config push`. The full walkthrough, including DNS and the compose file, is in [Quick start](docs/quick-start.md).

### Documentation

- [Documentation index](docs/README.md)
- [Install](docs/install.md)
- [Quick start](docs/quick-start.md)
- [Add a server](docs/guides/add-a-server.md)
- [Add an app](docs/guides/add-an-app.md)
- [Deploy an app](docs/guides/deploy-an-app.md)
- [ship.yaml reference](docs/reference/ship-yaml.md)
- [Commands](docs/reference/commands.md)
- [TUI](docs/reference/tui.md)

### Contributing

```sh
make check        # gofmt, go vet, go test, bash -n and shellcheck
```

See [Development](docs/development.md) for the code layout.
