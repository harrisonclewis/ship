# Configuration

`ship.yaml` lists your servers and the apps on each one. It is validated strictly before anything reaches a server, because its values end up on a shell line there. Start from [`examples/`](../examples), or add servers and apps from the [TUI](tui.md).

## Where ship looks

1. `SHIP_CONFIG`, if set. A missing file is an error.
2. `./ship.yaml` in the current directory.
3. `~/.config/ship/ship.yaml` (`$XDG_CONFIG_HOME/ship` on Linux when set, `%USERPROFILE%\.config\ship` on Windows).

Each app's `apps/<name>/` folder is read from beside the `ship.yaml` that was found. Apps with the same name on different servers share that folder.

## Shape

```yaml
backup_dir_local: ~/backups/vps

servers:
  prod:
    host: ops@203.0.113.10
    apps:
      hello: { … }
  staging:
    host: ops@203.0.113.20
    apps:
      hello: { … }
```

| Key | Meaning |
|---|---|
| `backup_dir_local` | where `ship <server> backup` downloads archives, under `<server>/<app>/` (default `~/backups/vps`) |
| `servers` | map of server name to server |

Server names follow the app name rules and can't be a command name (`setup`, `deploy`, `status`, …) or `all`.

## Servers

| Key | Meaning |
|---|---|
| `host` | ssh destination, `user@host` (`root@…` only for the first setup) |
| `identity_file` | optional SSH private key for this server, e.g. `~/.ssh/prod`; ssh offers only that key. Without it, ship uses your ssh config and agent |
| `admin_user` | sudo user created by `setup` (default `ops`; not `root` or `deploy`) |
| `arch` | `linux/amd64` (default) or `linux/arm64`; also selects the release asset |
| `caddy_image` | pinned Caddy image (a tag or sha256 digest is required) |
| `apps` | map of app name to app; may be empty |

## Apps

| Key | Meaning |
|---|---|
| `domains` | one or more FQDNs, all served; the first is canonical |
| `redirects` | optional FQDNs that permanently redirect to the canonical domain |
| `upstream` | `service:port` on the `web` network, e.g. `hello:8080` |
| `source` | `github-release` (`repo`, optional `asset` with `{arch}`/`{app}`) or `image` (`path` to a local git repo; `~/` works; may be empty until you need to build) |
| `uid` | optional system user/group and data owner (required for SQLite backups) |
| `data` | server directories the app owns |
| `health.http_path` | optional extra path checked from inside `web` after a switch |
| `backup` | `sqlite`, `files`, and/or `postgres: {service, db, user}` |
| `env` | non-secret values; uploaded to `.ship-env` and available to compose as `${VAR}` |
| `build.commands` | image apps only: a script run on your machine in `source.path` before the image is built |
| `deploy.commands` | a script run on the server in the new image just before it goes live |
| `deploy.maintenance` | show a 503 maintenance page while deploying (default `true`) |

Commands are YAML block scalars and run with `sh -e`, stopping at the first failure. Blank and `#` lines are fine:

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

See [Deploying](deploying.md#build-and-deploy-commands) for when each runs.

## Rules

- App names are lowercase letters, digits and dashes. `all` and `proxy` are reserved.
- Domains must be lowercase FQDNs and are unique across every app on every server, including redirects.
- Server paths must be clean and absolute.
- Env values are limited to letters, digits and `_./:@,+*%~=?-`. Put anything else, and every secret, in the server's `.env` with `ship <server> env edit <app>`.

## Compose files

Each app has a hand-written `apps/<name>/compose.yaml`. ship uploads it, plus `apps/<name>/Dockerfile` for release apps.

- Use `image: ${IMAGE}`; ship writes it to `/opt/apps/<name>/.ship.env`.
- A healthcheck is required: a deploy fails if the container has none or it never becomes healthy.
- Secrets come from `env_file: .env` with `required: false`.

See [Adding an app](apps.md) for the full checklist.
