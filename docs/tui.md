# TUI

Run `ship` with no arguments to open the interactive TUI. Add `--dry-run` to try every action without touching a server; a **DRY RUN** badge shows in the header.

## Servers

The Servers tab lists every server in `ship.yaml` with its IP, its apps and the proxy status, which loads in the background.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | open the server |
| `a` | add a server |
| `r` | refresh status |
| `Tab`, `1`/`2` | switch between Servers and Activity |
| `q` | quit |

## A server

The server page shows each app with its current and previous version, health and domains. `not deployed` marks apps that aren't on the server yet, `!` marks a missing `apps/<name>/compose.yaml`, and **MAINT** marks an app showing its maintenance page.

| Key | Action |
|---|---|
| `Enter` | the app's actions |
| `m` | all server actions |
| `d` | deploy all |
| `b` | back up all |
| `a` | add an app |
| `e` | edit the server |
| `r` | refresh |
| `Esc` | back |

**App actions:** deploy latest or build, deploy a version, roll back, restart, maintenance on/off, back up, list backups, restore, logs, edit env, edit, delete.

**Server actions:** deploy all, back up all, proxy update, setup, lock SSH, VPS update, reboot, add app, edit, delete.

Lock SSH, rollback, reboot, restore, deploy all and every delete ask first.

## Running actions

Each action opens a live, scrollable output view and is recorded in the Activity tab. Press `Esc` to go back while it keeps running; a toast says when it finishes. `Ctrl+C` in the output view cancels it, after asking.

One action runs per server at a time; actions on different servers run side by side. Quitting while something is running asks first.

**Logs** and **Edit env** hand the terminal to `ssh` and come back to the TUI when you close them.

## Editing

Adding or editing a server or app opens a form. A server's form takes its host and, optionally, the SSH private key to log in with. Build and deploy commands get a numbered multi-line editor: `alt+Enter` or `ctrl+j` adds a line, `ctrl+e` opens your `$EDITOR`.

Saving:

- checks the whole config first and refuses anything invalid,
- keeps your comments, key order and blank lines,
- keeps the previous file as `ship.yaml.bak`,
- refuses if `ship.yaml` changed on disk since the TUI loaded it,
- creates `~/.config/ship/ship.yaml` if you have no config yet.

Deleting a server or app only removes it from `ship.yaml`; nothing on the server changes. Edits are saved even with `--dry-run`, because they only touch your local file.

The TUI doesn't write compose files: add `apps/<name>/compose.yaml` yourself (see [Adding an app](apps.md)).
