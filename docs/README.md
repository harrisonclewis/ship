# ship documentation

Start with [Install](install.md), then [Quick start](quick-start.md).

## Get started

| Page | What it covers |
|---|---|
| [Install](install.md) | One-line installer, manual download, building from source, requirements |
| [Quick start](quick-start.md) | Install, add a server, run setup, create a sample image app and deploy it |

## Guides

One task per page.

| Guide | What it covers |
|---|---|
| [Set up SSH access](guides/ssh-access.md) | Create a key, add it to the server, add more machines or people |
| [Add a server](guides/add-a-server.md) | Add a server, run setup, lock SSH, server layout |
| [Add an app](guides/add-an-app.md) | Create a desired app on the server, write its compose file, Laravel example |
| [Deploy an app](guides/deploy-an-app.md) | Deploy one app or all, what a deploy does, failure handling, release and image apps |
| [Roll back](guides/roll-back.md) | Switch an app back to its previous image |
| [Manage domains](guides/manage-domains.md) | Domains, redirects, the canonical domain and proxy restart |
| [Change the app source](guides/change-app-source.md) | `source.type`, release repo and asset, local `sources` paths |
| [Edit config](guides/edit-config.md) | Local `ship.yaml`, server-backed app edits, compose and Dockerfile editing, `config push` |
| [Environment values and secrets](guides/environment-and-secrets.md) | Non-secret `env` in the app record, secrets in `.env` |
| [Build and deploy commands](guides/build-and-deploy-commands.md) | `build.commands`, build approval, `deploy.commands` |
| [Maintenance mode](guides/maintenance-mode.md) | Turn the maintenance page on or off |
| [Back up and restore](guides/back-up-and-restore.md) | Backup archives, schedule, retention, downloads, restore |
| [Use ship from another machine](guides/use-from-another-machine.md) | Server as source of truth, `diff`, local `sources` |
| [Run ship on the server](guides/run-on-the-server.md) | Local connection, sudo rules, local image builds, backups |
| [Deploy from CI](guides/deploy-from-ci.md) | The `deploy` user and a forced SSH command |
| [Update ship](guides/update-ship.md) | `ship update`, update checks, the server agent |

## Reference

| Page | What it covers |
|---|---|
| [ship.yaml](reference/ship-yaml.md) | Local server and source keys, where app config lives, validation rules |
| [Compose files](reference/compose.md) | What the server-side compose file must contain |
| [Commands](reference/commands.md) | Every CLI command, global flags and environment variables |
| [TUI](reference/tui.md) | Operational screens and keys; configuration screens are being updated |

## Project

| Page | What it covers |
|---|---|
| [Security](security.md) | Release verification, signed updates, backups, SSH and secrets |
| [Development](development.md) | Local build, checks and code layout |
| [Releasing](releasing.md) | Signing key, tag and release process |
