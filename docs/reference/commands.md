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
| `ship config edit` | edit the local `ship.yaml` |

## Server commands

| Command | What it does |
|---|---|
| `ship <server> setup` | prepare or re-run the server from its desired settings |
| `ship <server> lock-ssh` | key-only SSH, no root login |
| `ship <server> vps update` | apt update, full-upgrade, autoremove |
| `ship <server> vps reboot` | reboot the server (with confirmation) |
| `ship <server> proxy sync` | render the Caddyfile from server records and reload Caddy with the desired `caddy_image` (`proxy update` is the old alias) |
| `ship <server> proxy restart` | render from server records, activate the desired `caddy_image` and recreate the Caddy container, applying every pending change |
| `ship <server> deploy <app> [version]` | deploy one app from its desired config; Git URL image sources also accept `commit:<sha>`, `release:<tag>` and `release:<tag>:<asset>`, and use the remote default branch when no version is given |
| `ship <server> deploy all` | deploy every desired app in name order |
| `ship <server> deploy … --allow-dirty` | allow an uncommitted image build |
| `ship <server> rollback <app>` | previous image; the database is not restored |
| `ship <server> status [app]` | version, previous version, health, config state, proxy and setup state |
| `ship <server> logs <app> [service]` | follow logs |
| `ship <server> restart <app>` | recreate the container |
| `ship <server> maintenance <on\|off> <app>` | serve or stop serving the maintenance page |
| `ship <server> env edit <app>` | `sudoedit /opt/apps/<app>/.env` |
| `ship <server> app set <app> <key> <value...>` | replace one desired app setting on the server |
| `ship <server> app add <app> <key> <value...>` | append domains or redirects |
| `ship <server> app remove <app> <key> <value...>` | remove domains or redirects |
| `ship <server> app edit <app>` | edit one app's desired record in `$VISUAL`/`$EDITOR` |
| `ship <server> app create <app>` | create a desired app from an editor template |
| `ship <server> app delete <app>` | delete desired config, stop the stack, keep data and `.env` |
| `ship <server> server set admin_user\|caddy_image <value>` | change a desired server-wide setting |
| `ship <server> config edit <app> [compose\|Dockerfile]` | edit a desired app file on the server |
| `ship <server> config push` | migrate an old full-format local file to server-side desired config |
| `ship <server> backup [app\|all] [--no-pull]` | archive now; download and verify locally |
| `ship <server> backups <app>` | list server archives |
| `ship <server> restore <app> <archive> [--yes]` | pre-restore backup, stop, restore, health-check |
| `ship <server> diff [app]` | compare desired with running config, plus proxy and setup state |

`status` without an app lists every desired app and the proxy. It prints a `config` column: `applied`, `pending deploy` or `not deployed`, plus health and version. For Git URL image apps it also prints the typed `ref` and `newer=yes latest=…` when the remote has moved; offline and authentication failures are not reported as newer. It also notes when the proxy is out of sync or setup is pending. `backup` without an app backs up every running app. `deploy all` keeps going when an app fails, prints a summary and exits non-zero if any failed. A local archive path passed to `restore` is uploaded first (unless the server uses a local connection).

### Local connections

Every server command also works with `connection: local`, where ship runs on the server itself and executes the agent directly instead of over SSH:

- `env edit` uses the local `sudoedit`.
- `logs` streams from the local Docker daemon.
- `backup --pull` copies archives into `backup_dir_local` instead of downloading them.
- `restore` takes a local archive without an SSH upload (ship stages it in `/tmp` for the agent).
- `deploy` builds image apps on this machine, warns that the build uses server CPU and memory, and verifies the image in the local Docker daemon without streaming it.

With no `user`, or when it matches the current user, privileged commands use `sudo -n`; a different `user` runs through `sudo -u <user>`. Running ship as root needs no sudo. Interactive commands may prompt for a password; non-interactive ones fail without passwordless sudo. See [Run ship on the server](../guides/run-on-the-server.md).

`diff` compares desired with running config for each app and also reports server-level proxy and setup differences. With no app and no differences it prints `No differences.`

`app create` writes a small template and a minimal compose file; edit the compose file next:

```sh
ship prod app create blog
ship prod config edit blog compose
```

`config push` is the only command that accepts an old full-format local `ship.yaml`. Run it once per server. It uploads desired config, keeps apps applied when they already match the running record, and rewrites that server's local block with a `.bak`.

## Build script approval

Build commands are part of the desired app record and are read from the server. When a script is new or has changed, ship prints it and asks for approval once on this machine:

- `--yes` approves without a prompt.
- Without a TTY, an unapproved script fails unless you pass `--yes`.
- Approvals are stored in `build-approvals.yaml` under the ship config directory, not in `ship.yaml`.
- `--dry-run` prints the script without running it.

## Concurrent edits

Config commands load the desired state first, then send back its hash. If another machine changed the app or server settings in between, the save fails with `changed on the server since you loaded it` and changes nothing. Run the command again to reload.

## Global flags

| Flag | Effect |
|---|---|
| `--dry-run` | print the ssh, sudo and docker commands instead of running them; in the TUI, every action |
| `--yes`, `-y` | skip confirmations, and approve build scripts non-interactively |
| `--version` | print the ship version |

Global flags may come before or after the command: `ship --dry-run prod deploy hello` and `ship prod rollback hello --yes` both work.

Destructive actions (`lock-ssh`, `rollback`, `vps reboot`, `restore`, `app delete`) prompt on a TTY and need `--yes` otherwise. `vps update` also asks before rebooting when a reboot is required.

## Local edits and server edits

Only these commands change local files without contacting a server:

- `ship config edit` edits the local `ship.yaml`.
- `ship <server> app set <app> source.path <directory|git-url>` writes `servers.<name>.sources.<app>` in the local `ship.yaml`; an empty value clears it.
- `ship <server> app set <app> source.build_dir <directory>` writes `servers.<name>.build_dirs.<app>` for an existing Git URL source; an empty value clears it and a relative directory inside the checkout is required.

Every other app and server command works against the server. `--dry-run` prints the steps instead of changing anything. See [Edit config](../guides/edit-config.md).

Saving a domain or redirect change only updates desired config. Apply it with:

```sh
ship prod proxy restart
```

Other record, compose and Dockerfile changes apply on the next deploy.

## Environment

| Variable | Effect |
|---|---|
| `SHIP_CONFIG` | path to `ship.yaml`; see [where ship looks](ship-yaml.md#where-ship-looks) |
| `SHIP_NO_UPDATE_CHECK=1` | turns off the daily [update check](../guides/update-ship.md) |
| `$VISUAL`, `$EDITOR` | editor for `ship config edit`, `app create|edit` and server config edits; defaults to `vi` (`notepad` on Windows) |

See [ship.yaml](ship-yaml.md) for config keys and [TUI](tui.md) for keys.
