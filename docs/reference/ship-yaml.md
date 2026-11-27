# ship.yaml reference

`ship.yaml` is the small, machine-specific file ship reads locally. It records how to reach each server — over SSH, or locally when ship runs on the server itself — and where each image app's checkout lives on this machine. Everything else about an app — its record, `compose.yaml` and `Dockerfile` — lives on the server, which is the source of truth.

The file is validated strictly before anything reaches a server, because its values end up on a shell line there. Unknown keys, a second YAML document and invalid values are rejected.

Start from [`examples/ship.yaml`](../../examples/ship.yaml), or follow [Add a server](../guides/add-a-server.md) to create it.

## Where ship looks

1. `SHIP_CONFIG`, if set. A missing file is an error.
2. `./ship.yaml` in the current directory.
3. `~/.config/ship/ship.yaml` (`$XDG_CONFIG_HOME/ship` on Linux when set, `%USERPROFILE%\.config\ship` on Windows).

A local `apps/<name>/` folder is no longer read. App records, compose files and Dockerfiles are edited on the server; see [where app config lives](#where-app-config-lives).

## Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `backup_dir_local` | string | `~/backups/vps` | where `ship <server> backup` saves archives, under `<server>/<app>/`; with a local connection this is a local copy |
| `servers` | map | — | server name to server |
| `servers.<name>.connection` | string | `remote` | `remote` reaches the server over SSH; `local` runs the agent on this machine |
| `servers.<name>.host` | string | required for `remote` | SSH destination, `user@host` (`root@…` only for the first setup); not allowed for `local` |
| `servers.<name>.identity_file` | string | empty | optional SSH private key for a remote server, e.g. `~/.ssh/prod`; ssh offers only that key. Without it, ship uses your ssh config and agent. Not allowed for `local` |
| `servers.<name>.user` | string | empty | for `local`, the optional admin user to act as, e.g. `ops`; empty uses the current user. Not allowed for `remote` |
| `servers.<name>.sources` | map | empty | app name to the local checkout used to build an image app, e.g. `web: ~/code/web` |

Only these keys are accepted. `admin_user`, `arch`, `caddy_image` and `apps` are no longer local keys:

- `admin_user` and `caddy_image` are server settings, changed with `ship <server> server set`.
- `arch` is detected from `uname -m` on the server.
- App records are created and edited on the server.

## Shape

```yaml
backup_dir_local: ~/backups/vps

servers:
  prod:
    host: ops@203.0.113.10
    identity_file: ~/.ssh/prod
    sources:                     # local checkouts, image apps only
      web: ~/code/web

  self:
    connection: local            # ship runs on this server
    user: ops                    # optional admin user to act as
    sources:
      web: /srv/code/web
```

`sources.<app>` is used only when building an image app on this machine. It is not uploaded and is not part of the app's server record.

A local connection has no `host` or `identity_file` and never stores a password. See [Run ship on the server](../guides/run-on-the-server.md) for the sudo rules, local installs, image builds and backups.

## Where app config lives

The desired configuration is authoritative and is edited immediately by ship. The running configuration is what the last successful deploy promoted.

| Path | What |
|---|---|
| `/opt/apps/<app>/.ship/desired/ship-app.yaml` | desired app record |
| `/opt/apps/<app>/.ship/desired/compose.yaml` | desired compose file |
| `/opt/apps/<app>/.ship/desired/Dockerfile` | desired Dockerfile, release apps only |
| `/opt/apps/.ship/server.yaml` | desired `admin_user` and `caddy_image` |
| `/opt/apps/<app>/ship-app.yaml` | running record, promoted by a successful deploy |
| `/opt/apps/<app>/compose.yaml` | running compose file |
| `/opt/apps/<app>/Dockerfile` | running Dockerfile, release apps only |
| `/opt/apps/<app>/.ship-meta` | running app metadata |

Edit desired config with [`ship <server> config edit <app>`](commands.md), `ship <server> app create|edit|set`, or `ship <server> server set`. A successful [`deploy`](../guides/deploy-an-app.md) promotes desired to running.

Domain and redirect edits only update desired config. They go live after `ship <server> proxy restart`; ship prints that reminder when you save them.

## App record keys

These keys live in the desired app record on the server, not in `ship.yaml`. Edit them with `ship <server> app create|edit|set`.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `domains` | list | required | one or more FQDNs, all served; the first is canonical |
| `redirects` | list | empty | optional FQDNs that permanently redirect to the canonical domain |
| `upstream` | string | required | `service:port` on the `web` network, e.g. `hello:8080` |
| `source.type` | string | required | `github-release` or `image` |
| `source.repo` | string | — | `owner/repo`; required for `github-release`, not allowed for `image` |
| `source.asset` | string | `{app}_linux_{arch}` | release asset pattern; must contain `{arch}`; `github-release` only |
| `uid` | integer | empty | optional system user/group and data owner; required for SQLite backups |
| `data` | list | empty | server directories the app owns |
| `health.http_path` | string | empty | optional extra path checked from inside `web` after a switch |
| `backup.sqlite` | list | empty | SQLite database files |
| `backup.files` | list | empty | files or directories copied with owners and modes |
| `backup.postgres.service` | string | — | compose service running Postgres |
| `backup.postgres.db` | string | — | database name |
| `backup.postgres.user` | string | — | database user |
| `env` | map | empty | non-secret values written to `.ship-env` and available to compose as `${VAR}` |
| `build.commands` | string | empty | image apps only: a script run on your machine in `sources.<app>` before the image is built |
| `deploy.commands` | string | empty | a script run on the server in the new image just before it goes live |
| `deploy.maintenance` | boolean | `true` | show a 503 maintenance page while deploying |

A successful deploy promotes the desired record to `/opt/apps/<app>/ship-app.yaml`. The server record never contains `source.path`; an image app's local checkout lives only in `servers.<name>.sources.<app>`.

### Changing server settings

`admin_user` and `caddy_image` are stored in `/opt/apps/.ship/server.yaml`.

```sh
ship prod server set admin_user ops
ship prod server set caddy_image caddy:2.10-alpine
```

`setup` reads them, defaulting to `ops` and `caddy:2.10-alpine`. Changing `admin_user` needs `ship prod setup`; changing `caddy_image` needs `ship prod proxy restart`.

## Rules

- App and server names are 2–32 characters: lowercase letters, digits and dashes, starting with a letter and ending with a letter or digit.
- `all` and `proxy` are reserved app names. Server names can't be a command (`setup`, `deploy`, `status`, `config`, …) or `all`.
- `connection` is `remote` (the default) or `local`.
- A remote server needs `host` in `user@host` form; a local server forbids `host` and `identity_file`.
- `identity_file` must be an absolute path or start with `~/`; it is only valid for a remote server.
- `user` is only valid with `connection: local`. It follows the admin-user rules (`ops`, not `root` or `deploy`) and is optional.
- `sources.<app>` must be a clean absolute path or start with `~/`.
- `domains` and `redirects` must be lowercase FQDNs with at least one dot and no trailing dot.
- Every domain and redirect must be unique across the desired apps on that server.
- Server paths (`data`, `backup.sqlite`, `backup.files`) must be clean and absolute, use only letters, digits and `._/-`, and not be `/`.
- `source.path` is not allowed in a server record. For an image app, put the local checkout in `servers.<name>.sources.<app>`.
- `uid` is between 1 and 2147483647.
- Env names match `[A-Za-z_][A-Za-z0-9_]*`; `IMAGE` is reserved. Values may use only letters, digits and `_./:@,+*%~=?-`, up to 4096 characters. Put anything else, and every secret, in the server's `.env` with `ship <server> env edit <app>`.
- Scripts must use LF line endings, contain no NUL characters, and be at most 256 KiB.
- `health.http_path` starts with `/` and has no spaces, quotes or backslashes.
- `caddy_image` must pin a tag or a sha256 digest.
- `build.commands` is valid only for `source.type: image`.
- `{arch}` is `amd64` or `arm64`, detected from the server's `uname -m`.

Commands in the app record are YAML block scalars and run with `sh -e`, stopping at the first failure. Blank and `#` lines are fine:

```yaml
build:
  commands: |
    composer install --no-dev --no-interaction --prefer-dist --optimize-autoloader
    npm ci --audit false
    npm run build:ssr
deploy:
  commands: |
    php artisan migrate --force
```

These are saved to the desired record with `ship <server> app edit <app>`. See [Build and deploy commands](../guides/build-and-deploy-commands.md) for when each runs, including the per-machine approval for build scripts.

Quick changes to source, domains, redirects, build/deploy commands and the maintenance flag can be made with `ship <server> app set|add|remove`; see [Edit config](../guides/edit-config.md).

## Migrating an old full-format file

Normal loading rejects an old full-format file with a message pointing at `ship <server> config push`. That is the only command that accepts the old format:

```sh
ship prod config push
```

It uploads each app's record and its local `apps/<app>/` compose file (and Dockerfile), keeps apps applied when they already match the running record, and rewrites that server's local block to the new shape with a `.bak`. Run it once per server.

## Compose files

Each app has a hand-written compose file in its desired configuration on the server: `/opt/apps/<app>/.ship/desired/compose.yaml`.

```sh
ship prod config edit <app> compose
```

This fetches the desired file to a temporary copy, opens it in your editor, validates it and puts it back. Deploy to apply:

```sh
ship prod deploy <app>
```

`app create` writes a minimal compose file you can edit straight away. For release apps, edit the Dockerfile with `ship prod config edit <app> Dockerfile`.

See [Compose files](compose.md).
