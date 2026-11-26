# Commands

Run `ship` with no arguments for the [TUI](tui.md). Every action is also a command, for scripts and CI:

```text
ship <server> <command> [arguments]
```

With only one server in `ship.yaml`, the server name is optional.

```text
ship <server> setup                              prepare or re-run the server
ship <server> lock-ssh                           key-only SSH, no root login
ship <server> vps update                         apt update, full-upgrade, autoremove
ship <server> vps reboot                         reboot (with confirmation)
ship <server> proxy update                       render, validate, swap and reload the Caddyfile
ship <server> deploy <app> [version]             deploy one app
ship <server> deploy all                         deploy every app in name order
ship <server> deploy … --allow-dirty             allow an uncommitted image build
ship <server> rollback <app>                     previous image; the database is not restored
ship <server> status [app]                       current and previous version, health, proxy state
ship <server> logs <app> [service]               follow logs
ship <server> restart <app>                      recreate the container
ship <server> maintenance <on|off> <app>         serve or stop serving the maintenance page
ship <server> env edit <app>                     sudoedit /opt/apps/<app>/.env
ship <server> backup [app|all] [--no-pull]       archive now; download and verify locally
ship <server> backups <app>                      list server archives
ship <server> restore <app> <archive> [--yes]    pre-restore backup, stop, restore, health-check
ship <server> diff [app]                         compare local config with the server's records
ship <server> import <app…>|--all [--overwrite]  import apps from the server's records
ship config edit [app]                           edit ship.yaml or apps/<app>/compose.yaml locally
ship version                                     print the ship version
ship update [--check]                            update ship to the latest release
```

## Editing config files

`ship config edit` opens the found [`ship.yaml`](configuration.md#where-ship-looks) in your editor. `ship config edit <app>` opens `apps/<app>/compose.yaml` beside that file; if it does not exist yet, the editor starts empty and the file is created on save.

The editor is `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows); values such as `code --wait` are allowed. ship edits a temporary copy. If you leave it unchanged nothing is written. Otherwise the result is validated with the same rules as a TUI save (`ship.yaml`) or the compose checks, then the original is replaced and the previous file is kept as `.bak`. An invalid file is not written: ship prints the error and asks `Reopen editor? [Y/n]`, keeping your edits in the temporary copy if you reopen. A save is refused if the real file changed on disk while you were editing.

This command only touches local files, so it never contacts a server and `--dry-run` has no effect.

`status` without an app lists every app and the proxy. `backup` without an app backs up all apps. `deploy all` keeps going when an app fails, prints a summary and exits non-zero if any failed.

## Global flags

| Flag | Effect |
|---|---|
| `--dry-run` | print the ssh and docker commands instead of running them; in the TUI, every action |
| `--yes`, `-y` | skip confirmations |
| `--version` | print the ship version |

Global flags may come before or after the command: `ship --dry-run prod deploy hello` and `ship prod rollback hello --yes` both work.

Destructive actions (`lock-ssh`, `rollback`, `vps reboot`, `restore`) prompt on a TTY and need `--yes` otherwise.

## Environment

| Variable | Effect |
|---|---|
| `SHIP_CONFIG` | path to `ship.yaml`; see [where ship looks](configuration.md#where-ship-looks) |
| `SHIP_NO_UPDATE_CHECK=1` | turns off the daily [update check](updates.md) |
| `$VISUAL`, `$EDITOR` | editor for `ship config edit` and the TUI; defaults to `vi` (`notepad` on Windows) |
