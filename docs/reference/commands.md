# Commands reference

Run `ship` with no arguments for the [TUI](tui.md). Every action is also a command, for scripts and CI:

```text
ship <server> <command> [arguments]
```

With only one server in `ship.yaml`, the server name is optional.

## Ship commands

| Command | What it does |
|---|---|
| `ship` | open the TUI (TTY only) |
| `ship help`, `ship -h`, `ship --help` | print usage |
| `ship version`, `ship --version` | print the ship version |
| `ship update [--check]` | update ship to the latest release, or only check |
| `ship config edit [app]` | edit `ship.yaml`, or the app's `compose.yaml`, locally |

## Server commands

| Command | What it does |
|---|---|
| `ship <server> setup` | prepare or re-run the server |
| `ship <server> lock-ssh` | key-only SSH, no root login |
| `ship <server> vps update` | apt update, full-upgrade, autoremove |
| `ship <server> vps reboot` | reboot the server (with confirmation) |
| `ship <server> proxy update` | render, validate, swap and reload the Caddyfile |
| `ship <server> deploy <app> [version]` | deploy one app |
| `ship <server> deploy all` | deploy every app in name order |
| `ship <server> deploy … --allow-dirty` | allow an uncommitted image build |
| `ship <server> rollback <app>` | previous image; the database is not restored |
| `ship <server> status [app]` | current and previous version, health, proxy state |
| `ship <server> logs <app> [service]` | follow logs |
| `ship <server> restart <app>` | recreate the container |
| `ship <server> maintenance <on\|off> <app>` | serve or stop serving the maintenance page |
| `ship <server> env edit <app>` | `sudoedit /opt/apps/<app>/.env` |
| `ship <server> app set <app> <key> <value...>` | replace one app setting in `ship.yaml` |
| `ship <server> app add <app> <key> <value...>` | append domains or redirects |
| `ship <server> app remove <app> <key> <value...>` | remove domains or redirects |
| `ship <server> app edit <app>` | edit one app's YAML in `$VISUAL`/`$EDITOR` |
| `ship <server> backup [app\|all] [--no-pull]` | archive now; download and verify locally |
| `ship <server> backups <app>` | list server archives |
| `ship <server> restore <app> <archive> [--yes]` | pre-restore backup, stop, restore, health-check |
| `ship <server> diff [app]` | compare local config with the server's records |
| `ship <server> import <app…>\|--all [--overwrite]` | import apps from the server's records |

`status` without an app lists every app and the proxy. `backup` without an app backs up all apps. `deploy all` keeps going when an app fails, prints a summary and exits non-zero if any failed. A local archive path passed to `restore` is uploaded first.

## Global flags

| Flag | Effect |
|---|---|
| `--dry-run` | print the ssh and docker commands instead of running them; in the TUI, every action |
| `--yes`, `-y` | skip confirmations |
| `--version` | print the ship version |

Global flags may come before or after the command: `ship --dry-run prod deploy hello` and `ship prod rollback hello --yes` both work.

Destructive actions (`lock-ssh`, `rollback`, `vps reboot`, `restore`) prompt on a TTY and need `--yes` otherwise. `vps update` also asks before rebooting when a reboot is required.

## Local edits

`ship config edit`, `ship <server> app set|add|remove|edit` and the TUI's config screens only touch local files. They never contact a server, and `--dry-run` has no effect on them. See [Edit config](../guides/edit-config.md).

## Environment

| Variable | Effect |
|---|---|
| `SHIP_CONFIG` | path to `ship.yaml`; see [where ship looks](ship-yaml.md#where-ship-looks) |
| `SHIP_NO_UPDATE_CHECK=1` | turns off the daily [update check](../guides/update-ship.md) |
| `$VISUAL`, `$EDITOR` | editor for `ship config edit` and the TUI; defaults to `vi` (`notepad` on Windows) |

See [ship.yaml](ship-yaml.md) for config keys and [TUI](tui.md) for keys.
