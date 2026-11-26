# TUI reference

Run `ship` with no arguments to open the interactive TUI. Add `--dry-run` to try every action without touching a server; a **DRY RUN** badge shows in the header. The header also shows the config file name.

| Key | Where | Action |
|---|---|---|
| `q` | outside a form or dialog | quit |
| `ctrl+c` | outside a form or dialog | quit, or cancel the running output (after asking) |
| `Tab`, `Shift+Tab`, `1`, `2` | Servers / Activity | switch tabs |

Quitting while something is running asks first.

## Servers tab

The Servers tab lists every server in `ship.yaml` with its IP, its apps and the proxy status, which loads in the background.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | open the server |
| `a` | add a server |
| `e` | edit `ship.yaml` |
| `r` | refresh status |
| `Tab`, `Shift+Tab`, `1`/`2` | switch between Servers and Activity |
| `q` | quit |

## Activity tab

Server actions are recorded here with their time, server, action, target and state.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | open the output |
| `Tab`, `Shift+Tab`, `1`/`2` | switch tabs |
| `q` | quit |

## A server

The server page shows each app with its current and previous version, health and domains. `not deployed` marks apps that aren't on the server yet, `!` marks a missing `apps/<name>/compose.yaml`, **MAINT** marks an app showing its maintenance page, **DIFF** means it differs from the last deploy, and **on server** marks an app that exists only on the server.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | the app's actions |
| `m` | all server actions |
| `d` | deploy all (asks first) |
| `b` | back up all |
| `p` | proxy update |
| `s` | setup |
| `l` | lock SSH (asks first) |
| `u` | VPS update |
| `R` | reboot (asks first) |
| `a` | add an app |
| `e` | edit the server |
| `x` | delete the server from `ship.yaml` (asks first) |
| `r` | refresh |
| `Esc` | back |
| `q` | quit |

## App actions

| Key | Action |
|---|---|
| `d` | deploy latest/build |
| `v` | deploy a version… |
| `o` | roll back (asks first) |
| `s` | restart |
| `m` | maintenance on/off |
| `b` | back up |
| `L` | list backups |
| `t` | restore (asks, then opens the restore picker) |
| `g` | follow logs; asks for an optional service |
| `e` | edit the secret `.env` |
| `i` | the **Edit…** submenu |
| `f` | edit `apps/<name>/compose.yaml` |
| `x` | delete the app from `ship.yaml` (asks first) |
| `c` | compare with server (only when the app is on the server) |

`↑`/`↓`, `j`/`k` move, `Enter` runs, `Esc` goes back.

Apps that exist only on the server show one action instead: **Import app** (`i`), or **Import compose file** when there is no record or an unreadable one. See [Use ship from another machine](../guides/use-from-another-machine.md).

## Edit… submenu

| Key | Action |
|---|---|
| `s` | **Source**: the local image path, or the release repo and asset |
| `d` | **Domains & redirects** |
| `c` | **Build & deploy commands** and the maintenance page |
| `y` | **Edit app YAML**: only this app's mapping |
| `a` | **All settings**: the full app form, including the name and source type |

The quick forms are prefilled and save through the same validation as the full form. **Edit app YAML** opens the app's mapping, comments included, with a header naming the server and app. The app name is fixed there; rename it with **All settings**. Invalid YAML shows the editor error and offers to reopen or discard, keeping the temporary copy while you decide.

## Server actions screen

`m` opens the same actions as a list.

| Key | Action |
|---|---|
| `d` | deploy all |
| `b` | backup all |
| `p` | proxy update |
| `s` | setup |
| `l` | lock SSH |
| `u` | VPS update |
| `R` | reboot |
| `a` | add app |
| `e` | edit server |
| `x` | delete server |

`↑`/`↓`, `j`/`k` move, `Enter` runs, `Esc` goes back. Lock SSH, reboot, rollback, restore, deploy all and every delete ask first.

## Restore picker

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | restore the selected archive, or open the local path field on the last entry |
| `p` | enter a local archive path |
| `r` | refresh the list |
| `Esc` | back |

Restore asks for confirmation. A local `.tar.gz` is uploaded before the restore.

## Compare

| Key | Action |
|---|---|
| `u` | use the server version (asks first) |
| `k`, `Esc` | keep mine |
| `↑`/`↓` | scroll |
| `q` | quit |

**Use server version** replaces your app config and files with the server's; **Keep mine** leaves them and the next deploy uploads yours. See [Use ship from another machine](../guides/use-from-another-machine.md).

## Running actions

Actions that run on a server open a live, scrollable output view and are recorded in the Activity tab. Press `Esc` to go back while it keeps running; a toast says when it finishes. `Ctrl+C` in the output view cancels it, after asking. `↑`/`↓` scroll.

One action runs per server at a time; actions on different servers run side by side. Quitting while something is running asks first.

**Logs** and **Edit env** hand the terminal to `ssh` and come back to the TUI when you close them.

## Forms

| Key | Action |
|---|---|
| `Enter` | next field / finish |
| `Tab` | next field |
| `Esc` | cancel |
| `alt+Enter`, `ctrl+j` | new line in a multi-line command field |
| `ctrl+e` | open the command field in your `$EDITOR` |

Adding or editing a server or app opens a form. A server's form takes its name, host, optional SSH private key, admin user, architecture and Caddy image. An app's full form takes its name, domains, redirects, upstream, UID, data directories, health path, environment, source, backup spec, build/deploy commands and maintenance page.

## Editing from the TUI

| Key | Opens |
|---|---|
| `e` on the Servers tab | `ship.yaml` |
| `f` in an app's actions | `apps/<name>/compose.yaml` |
| `y` in Edit… | only the app's YAML mapping |
| `a` in Edit… | the full app form |

File edits use your `$VISUAL`, `$EDITOR` or `vi` (`notepad` on Windows) through a temporary copy, and return to the TUI when the editor closes. If there are no changes, nothing is written. If there are, the file is validated; an invalid `ship.yaml` or compose file shows the error and offers to reopen the editor or discard the changes, keeping the temporary copy while you decide. A missing compose file opens empty and is created on save.

Saving:

- checks the whole config first and refuses anything invalid,
- keeps your comments, key order and blank lines,
- keeps the previous file as `.bak`,
- refuses if the file changed on disk since the TUI loaded or opened it,
- creates `~/.config/ship/ship.yaml` if you have no config yet.

Deleting a server or app only removes it from `ship.yaml`; nothing on the server changes. Edits are saved even with `--dry-run`, because they only touch your local files.

## Confirmation dialogs

`Enter` or `y` confirms; `Esc`, `n` or `q` cancels. A notice closes with `Enter`.

See [Edit config](../guides/edit-config.md) for the config screens and [Compose files](compose.md) for what a compose file needs.
