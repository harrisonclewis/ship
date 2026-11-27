# Edit config

App configuration lives on the server. ship edits desired config there and runs a deploy to apply it. The only local configuration file is [`ship.yaml`](../reference/ship-yaml.md), which holds server connection details and image app source paths.

## Edit the local ship.yaml

```sh
ship config edit
```

This opens the found [`ship.yaml`](../reference/ship-yaml.md#where-ship-looks) in `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows). Values such as `code --wait` are allowed.

ship edits a temporary copy. If you leave it unchanged, nothing is written. Otherwise the result is validated, the original is replaced and the previous file is kept as `.bak`.

An invalid file is not written: ship prints the error and asks `Reopen editor? [Y/n]`, keeping your edits in the temporary copy if you reopen. A save is refused if the real file changed on disk while you were editing.

## Change one app setting

```sh
ship prod app set <app> <key> <value...>
ship prod app add <app> <key> <value...>
ship prod app remove <app> <key> <value...>
```

The server name is required because the app is stored on that server; with exactly one server it is optional.

`set` replaces a value. Its keys are:

| Key | Value |
|---|---|
| `source.type` | `github-release` or `image` |
| `source.path` | local directory or Git repository URL for an image app; writes local `sources.<app>`, not the server |
| `source.repo` | GitHub `owner/repo` |
| `source.asset` | release asset pattern; empty restores the default |
| `domains` | one or more domains, replacing the list |
| `redirects` | one or more redirects, replacing the list |
| `build.commands` | script; `-` reads it from stdin |
| `deploy.commands` | script; `-` reads it from stdin |
| `deploy.maintenance` | `true` or `false`; empty restores the default `true` |

An unknown key lists the valid ones. For `build.commands` and `deploy.commands`, a value of `-` reads the script from stdin:

```sh
ship prod app set hello deploy.commands - < deploy.sh
```

`source.path` is special: it edits the local `servers.prod.sources.hello` entry and never contacts the server. It accepts a directory or a supported Git URL. An empty value clears it:

```sh
ship prod app set web source.path ~/code/web
ship prod app set api source.path https://github.com/acme/api.git
ship prod app set web source.path ""
```

Every other key updates the desired record on the server immediately.

`add` and `remove` work only on `domains` and `redirects` and accept one or more values. `add` appends missing values in order; values already present are skipped with a note. `remove` is atomic: if any value is missing it changes nothing and exits non-zero. Removing the last domain is an error; removing the first (canonical) domain is allowed and prints the new canonical domain. Duplicate values in one command are collapsed. Domains and redirects are validated against the other desired apps on that server.

Saving prints what to do next:

- Domain or redirect changes: `Restart Caddy to apply: ship prod proxy restart`.
- Other changes: `Deploy to apply: ship prod deploy <app>`.

## Edit one app's record

```sh
ship prod app edit <app>
```

This fetches the desired record to a temporary file and opens it in `$VISUAL`, `$EDITOR` or `vi`. The file starts with the full record, including `uid`, `data`, `health`, `backup` and `env`, which have no `app set` keys:

```yaml
domains: [hello.example.com]
redirects: [www.hello.example.com]
upstream: hello:8080
source:
  type: github-release
  repo: example/hello
  asset: hello_linux_{arch}
uid: 10001
data: [/srv/hello]
health:
  http_path: /health
backup:
  sqlite: [/srv/hello/hello.db]
env:
  HELLO_DB: /data/hello.db
```

The record is validated against every other desired app on the server before it is saved. A deploy applies it. Changing `domains` or `redirects` here also only updates desired config; run `ship prod proxy restart` to make routing live.

## Create and delete apps

```sh
ship prod app create blog
ship prod app delete blog
```

`app create` opens a template with `domains`, `upstream`, `source` and `backup`, then writes the desired record and a minimal compose file. It fails if the app already exists. Next:

```sh
ship prod config edit blog compose
```

`app delete` asks first. It removes the desired configuration and stops the app's stack if it was deployed. Data directories and `/opt/apps/<app>/.env` are kept. Outside a TTY, pass `--yes`. Run `ship prod proxy restart` afterwards to drop its routes from Caddy.

## Edit compose and Dockerfile

```sh
ship prod config edit blog compose
ship prod config edit blog Dockerfile
```

ship fetches the desired file to a temporary copy and opens it in your editor. Compose files are validated with the [compose rules](../reference/compose.md); Dockerfiles are not validated. The previous temporary copy is discarded if you make no changes, and a concurrent change on the server refuses the save.

A save prints `Deploy to apply: ship prod deploy <app>`. Deploy is what applies compose, record and Dockerfile changes.

## Server settings

```sh
ship prod server set admin_user ops
ship prod server set caddy_image caddy:2.10-alpine
```

`admin_user` applies with `ship prod setup`; `caddy_image` applies with `ship prod proxy restart`.

## Migrate an old local file

If `ship.yaml` still has the old full format — local `apps`, `admin_user`, `arch` or `caddy_image` — normal commands reject it with a message pointing at `ship config push` (or `ship <server> config push` when several servers are configured).

Run once per server:

```sh
ship prod config push
```

This uploads each app's record and its `apps/<app>/compose.yaml` (and `Dockerfile`) from the old local format, keeps apps applied when they already match the running record, and rewrites that server's block to the new shape with a `.bak`. If the server has no recorded setup state, it also records the pushed admin user and app uids as already applied, so the migration does not force a full re-setup. Only `config push` accepts the old format.

## Concurrent edits

Config commands carry the hash of the desired state they loaded. If another machine changes the same app or server settings first, the save fails with `changed on the server since you loaded it` and nothing is written. Run the command again to reload and retry.

The TUI's configuration screens are being updated for the server-backed model in a later change. Until then, use these commands for app and server settings.
