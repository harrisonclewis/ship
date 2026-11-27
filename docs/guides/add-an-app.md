# Add an app

An app is a desired record plus a compose file (and, for release apps, a Dockerfile) stored on the server. Create it from the CLI; there is no local `apps/<name>/` folder.

## 1. Create the desired record

```sh
ship prod app create hello
```

This opens a template in your editor:

```yaml
domains: [hello.example.com]
upstream: hello:8080
source:
  type: image
backup: {}
```

The TUI offers **Source directory**, **Git repository URL** and **GitHub release asset**. For a release-asset app, change `source` to `type: github-release` with `repo` and optional `asset`. For an image app built from a directory or Git URL, point this machine at the input:

```sh
ship prod app set hello source.path ~/code/hello
ship prod app set hello source.path https://github.com/acme/hello.git
```

| Key | What it does |
|---|---|
| `domains` | one or more FQDNs, all served; the first is canonical |
| `redirects` | optional FQDNs that permanently redirect to the canonical domain |
| `upstream` | `service:port` on the `web` network |
| `source` | `github-release`, or `image` with a local directory or Git URL in `sources.<app>` |
| `uid` | optional app user and data owner; required for SQLite backups |
| `data` | server directories the app owns |
| `health.http_path` | optional extra path checked from inside `web` after a switch |
| `backup` | SQLite, files and/or Postgres |
| `env` | non-secret values for compose |
| `build.commands`, `deploy.commands`, `deploy.maintenance` | scripts and the maintenance page |

Use a lowercase name made of letters, digits and dashes; `all` and `proxy` are reserved. Apps with the same name on different servers have independent records.

`app create` also writes a minimal compose file. To change the record later:

```sh
ship prod app edit hello
```

This opens the full record in `$VISUAL`/`$EDITOR`, validates it against the server's other apps and saves it as desired config. See [ship.yaml](../reference/ship-yaml.md#app-record-keys) for every key and its rules.

## 2. Write the compose file

```sh
ship prod config edit hello compose
```

Use:

```yaml
services:
  hello:
    image: ${IMAGE}
    restart: unless-stopped
    volumes:
      - /srv/hello:/data
    environment:
      HELLO_DB: ${HELLO_DB:-/data/hello.db}
    env_file:
      - path: .env
        required: false
    networks: [web]
    mem_limit: 256m
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/health"]
      interval: 10s
      timeout: 3s
      start_period: 10s
      retries: 3

networks:
  web:
    external: true
```

- The service in `upstream` must be the one that listens on that port; it is also where deploy commands run.
- Use `image: ${IMAGE}`. ship writes the deploy image to `/opt/apps/<name>/.ship.env`.
- A healthcheck is required: a deploy fails if the container has none or never becomes healthy.
- Keep the service on the external `web` network and never publish ports. Docker's published ports bypass ufw.
- Give it a `mem_limit`.
- Get secrets from `env_file: .env` with `required: false`.

For a `github-release` app, also create its Dockerfile:

```sh
ship prod config edit hello Dockerfile
```

The release binary is copied in as `BINARY`. See [Compose files](../reference/compose.md).

## 3. Add data and backups

Set these in the record with `ship prod app edit hello`:

```yaml
uid: 10001
data: [/srv/hello]
backup:
  sqlite: [/srv/hello/hello.db]
```

Point the compose volume at the data path. Use `files` for directories and `postgres` for a compose service. See [Back up and restore](back-up-and-restore.md).

Changing `uid` leaves setup pending until you run `ship prod setup`, which creates the app user.

## 4. Add commands if needed

```sh
ship prod app set hello build.commands - < build.sh
ship prod app set hello deploy.commands - < deploy.sh
```

Or edit them in `ship prod app edit hello`:

```yaml
build:
  commands: |
    npm ci --audit false
    npm run build
deploy:
  commands: |
    php artisan migrate --force
```

`build.commands` run on your machine in `sources.hello` before an image build; `deploy.commands` run on the server in the new image just before it goes live. A new or changed build script is printed and needs approval once on this machine. See [Build and deploy commands](build-and-deploy-commands.md).

## 5. Deploy

```sh
ship prod deploy hello
ship prod proxy sync
```

For a new app, deploy first so it has a running upstream, then sync the proxy to add its route. Later domain or redirect changes need:

```sh
ship prod proxy restart
```

A deploy applies record, compose and Dockerfile changes; it does not apply pending domain changes to Caddy.

## 6. Put secrets on the server

Keep secrets out of the app record. Put them in `/opt/apps/<name>/.env`:

```sh
ship prod env edit hello
ship prod restart hello
```

See [Environment values and secrets](environment-and-secrets.md).

## Example: Laravel

A Laravel app usually runs one image as three services:

| Service | Runs |
|---|---|
| `lewagent` | the web server, with the `/up` healthcheck |
| `queue` | `php artisan queue:work` |
| `scheduler` | `php artisan schedule:work` |

With `upstream: lewagent:8080`, the web service must be named `lewagent`. All three use `${IMAGE}`. Install dependencies and build assets before the image build, and migrate before the switch:

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

A Postgres service is backed up with:

```yaml
backup:
  postgres: { service: db, db: lewagent, user: lewagent }
```

## Examples

[`examples/ship.yaml`](../../examples/ship.yaml) shows the local server and sources shape. The compose and Dockerfile files under `examples/apps/` show what a release or image app needs; they are not read from a local app folder.
