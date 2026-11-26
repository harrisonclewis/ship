# ship documentation

Start with [Install](install.md), then [Quick start](quick-start.md).

## Get started

| Page | What it covers |
|---|---|
| [Install](install.md) | One-line installer, manual download, building from source, requirements |
| [Quick start](quick-start.md) | Install, add a server, run setup, add a sample image app and deploy it |

## Guides

One task per page.

| Guide | What it covers |
|---|---|
| [Add a server](guides/add-a-server.md) | Add a server, run setup, lock SSH, server layout |
| [Add an app](guides/add-an-app.md) | Add an app to `ship.yaml`, write its compose file, Laravel example |
| [Deploy an app](guides/deploy-an-app.md) | Deploy one app or all, what a deploy does, failure handling, release and image apps |
| [Roll back](guides/roll-back.md) | Switch an app back to its previous image |
| [Manage domains](guides/manage-domains.md) | Domains, redirects, the canonical domain and proxy updates |
| [Change the app source](guides/change-app-source.md) | `source.path`, GitHub release repo and asset |
| [Edit config](guides/edit-config.md) | `ship config edit`, `ship app set/add/remove/edit`, TUI editing |
| [Environment values and secrets](guides/environment-and-secrets.md) | Non-secret `env`, secrets in `.env` |
| [Build and deploy commands](guides/build-and-deploy-commands.md) | `build.commands` and `deploy.commands` |
| [Maintenance mode](guides/maintenance-mode.md) | Turn the maintenance page on or off |
| [Back up and restore](guides/back-up-and-restore.md) | Backup archives, schedule, retention, downloads, restore |
| [Use ship from another machine](guides/use-from-another-machine.md) | Server records, `diff`, `import`, setting `source.path` |
| [Deploy from CI](guides/deploy-from-ci.md) | The `deploy` user and a forced SSH command |
| [Update ship](guides/update-ship.md) | `ship update`, update checks, the server agent |

## Reference

| Page | What it covers |
|---|---|
| [ship.yaml](reference/ship-yaml.md) | Every config key, defaults and validation rules |
| [Compose files](reference/compose.md) | What `apps/<name>/compose.yaml` must contain |
| [Commands](reference/commands.md) | Every CLI command, global flags and environment variables |
| [TUI](reference/tui.md) | Every screen and key |

## Project

| Page | What it covers |
|---|---|
| [Security](security.md) | Release verification, signed updates, backups, SSH and secrets |
| [Development](development.md) | Local build, checks and code layout |
| [Releasing](releasing.md) | Signing key, tag and release process |
