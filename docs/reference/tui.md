# TUI reference

The server is the source of truth. The TUI reads each server's desired app configuration over SSH, or directly on this machine for a `connection: local` server, and writes changes back through the same operations as the CLI, so it needs the new local [`ship.yaml`](../reference/ship-yaml.md) format (server connection details and image checkouts only). An old full-format file is rejected with a message telling you to run `ship <server> config push`.

Run `ship` with no arguments to open the interactive TUI. Add `--dry-run` to print every action instead of touching a server; a **DRY RUN** badge shows in the header. The header also shows the config file name.

| Key | Where | Action |
|---|---|---|
| `q` | outside a form or dialog | quit |
| `ctrl+c` | outside a form or dialog | quit, or cancel the running output (after asking) |
| `Tab`, `Shift+Tab`, `1`, `2` | Servers / Activity | switch tabs |

Quitting while something is running asks first.

## Servers tab

The Servers tab lists every server in `ship.yaml` with its IP, apps and proxy status, which load in the background.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | open the server |
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

The server page shows the connection (`host`, or `local` and the optional user), the server's desired admin user, architecture and Caddy image, plus each app's current and previous version, health, domains and **CONFIG** badge. **MAINT** marks an app showing its maintenance page.

The **CONFIG** badge compares desired with running:

| Badge | Meaning |
|---|---|
| `applied` | running matches desired |
| `pending deploy` | desired changed; deploy the app |
| `not deployed` | the app has never been deployed |

For a Git URL source, **CURRENT** shows the recorded ref and a **NEW** badge appears when the remote default branch has moved past the running commit, or when a newer published release exists for a running release identity. The badge appears only after a successful deploy and never from a failed network or authentication check.

When anything is out of sync, an alert at the top of the page lists each item, for example `web: config changed, pending deploy`, `Caddy: domains changed, restart Caddy to apply (p)` or `Server: admin_user changed, run setup (s)`. It is hidden when everything is in sync.

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | the app's actions |
| `a` | add an app (desired config on the server) |
| `m` | all server actions |
| `d` | deploy all (asks first) |
| `p` | **Restart Caddy** (`proxy restart`); applies routing and image changes |
| `P` | **Sync Caddy (reload)** (`proxy sync`); reloads without recreating Caddy |
| `s` | setup |
| `g` | server settings (`admin_user`, `caddy_image`) |
| `l` | lock SSH (asks first) |
| `u` | VPS update |
| `R` | reboot (asks first) |
| `r` | refresh status and configuration |
| `Esc` | back |
| `q` | quit |

## App actions

| Key | Action |
|---|---|
| `d` | deploy latest/build |
| `v` | deploy a version…; for Git URL sources this opens the Commits/Releases picker |
| `o` | roll back (asks first) |
| `s` | restart |
| `m` | maintenance on/off |
| `b` | back up |
| `L` | list backups |
| `t` | restore (asks, then opens the restore picker) |
| `g` | follow logs; asks for an optional service |
| `e` | edit the secret `.env` |
| `i` | Edit… (source, domains, commands, YAML or all settings) |
| `f` | edit the server's desired `compose.yaml` |
| `F` | edit the server's desired `Dockerfile` (release apps) |
| `c` | compare running with desired |
| `x` | delete the app (asks first) |

`↑`/`↓`, `j`/`k` move, `Enter` runs, `Esc` goes back. The app actions page shows the app's **CONFIG** and health badges.

## Server actions screen

`m` opens the same actions as a list. It includes deploy all, backup all, **Restart Caddy**, **Sync Caddy (reload)**, setup, server settings, lock SSH, VPS update, reboot, add app, edit server and delete server.

`↑`/`↓`, `j`/`k` move, `Enter` runs, `Esc` goes back. Lock SSH, reboot, rollback, restore, delete and deploy all ask first.

## Editing configuration

The app list, app forms, quick Edit… menu, compose/Dockerfile editor and delete all read and write the server's desired configuration. Changes are saved with the hash loaded with the page. If another machine changed the same configuration first, the TUI says it changed on the server since you loaded it, saves nothing and reloads the latest version.

- **Server form** (`a` on Servers, or `e` on a server) edits the connection in the local `ship.yaml`. Choose **Remote** for `host` and `identity_file`, or **Local** for an optional admin `user`; the other fields are hidden.
- **Server settings** (`g` on a server) edits `admin_user` and `caddy_image` on the server. Saving may leave setup or Caddy pending; the alert says which key applies it.
- **App form** (`a` or Edit… → All settings) edits the desired record. Source offers **Source directory**, **Git repository URL** and **GitHub release asset**. The directory or URL is written only to this machine's `servers.<server>.sources.<app>`; the server record stays `source.type: image`. A Git URL has an optional **Build directory** field for a relative directory inside the checkout. Both Git source forms save the URL and directory together.
- **App YAML** (Edit… → Edit app YAML) edits the full server-side record in `$VISUAL`, `$EDITOR` or `vi`.
- **Commits/Releases picker** (Git URL source, `v`) lazy-loads the default branch's recent commits or published GitHub releases. Selecting a release then offers **Build source** or one of its downloadable assets; checksum files are hidden. Press `r` to refresh the open tab.
- **compose.yaml / Dockerfile** (`f`, `F`) edit the desired files on the server.
- Deleting an app removes its desired configuration and stops its stack. Data directories and `.env` are kept.

Every save that changes the server refreshes the configuration badges and the server alert. Deploy, Caddy and setup actions do the same when they finish.

### Build approval

A deploy may stop because the server's build script for an image app has not been approved on this machine. The TUI shows the script in a scrollable dialog with **Approve and deploy** and **Cancel**. Approving records the approval locally and retries the deploy.

## Restore picker

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move |
| `Enter` | restore the selected archive, or open the local path field on the last entry |
| `p` | enter a local archive path |
| `r` | refresh the list |
| `Esc` | back |

Restore asks for confirmation. A local `.tar.gz` is uploaded before the restore.

## Running actions

Actions that run on a server open a live, scrollable output view and are recorded in the Activity tab. Press `Esc` to go back while it keeps running; a toast says when it finishes. `Ctrl+C` in the output view cancels it, after asking. `↑`/`↓` scroll.

One action runs per server at a time; actions on different servers run side by side. Quitting while something is running asks first.

**Logs** and **Edit env** hand the terminal to `ssh` and come back to the TUI when you close them.

## Editing the local file

`e` on the Servers tab opens the local [`ship.yaml`](../reference/ship-yaml.md) in your `$VISUAL`, `$EDITOR` or `vi`.

Saving the local file:

- checks that it is the new local format and refuses anything invalid,
- keeps your comments, key order and blank lines,
- keeps the previous file as `.bak`,
- refuses if the file changed on disk since the TUI loaded or opened it,
- creates `~/.config/ship/ship.yaml` if you have no config yet.

Local edits are saved even with `--dry-run`.

## Confirmation dialogs

`Enter` or `y` confirms; `Esc`, `n` or `q` cancels. A notice closes with `Enter`. In the build-approval dialog, `↑`/`↓` scroll the script and `Enter`/`y` approves and deploys.

See [Edit config](../guides/edit-config.md) for app and server configuration and [Compose files](compose.md) for what a compose file needs.
