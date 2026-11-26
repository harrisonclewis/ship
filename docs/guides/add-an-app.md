# Add an app

An app is a block under its server's `apps:` in `ship.yaml` plus a hand-written `apps/<name>/compose.yaml`. Add it with an editor or with `a` on the server page in the [TUI](../reference/tui.md#a-server).

## 1. Add the app to ship.yaml

```yaml
servers:
  prod:
    apps:
      hello:
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

| Key | What it does |
|---|---|
| `domains` | one or more FQDNs, all served; the first is canonical |
| `redirects` | optional FQDNs that permanently redirect to the canonical domain |
| `upstream` | `service:port` on the `web` network |
| `source` | `github-release`, or `image` with a local `path` |
| `uid` | optional app user and data owner; required for SQLite backups |
| `data` | server directories the app owns |
| `health.http_path` | optional extra path checked from inside `web` after a switch |
| `backup` | SQLite, files and/or Postgres |
| `env` | non-secret values for compose |
| `build.commands`, `deploy.commands`, `deploy.maintenance` | scripts and the maintenance page |

See [ship.yaml](../reference/ship-yaml.md#keys) for every key and its rules.

Use a lowercase name made of letters, digits and dashes; `all` and `proxy` are reserved. Apps with the same name on different servers share `apps/<name>/`.

## 2. Write the compose file

Create `apps/<name>/compose.yaml` beside `ship.yaml`:

```yaml
# apps/hello/compose.yaml
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
- Use `image: ${IMAGE}`. ship writes `IMAGE` to `/opt/apps/<name>/.ship.env`.
- A healthcheck is required: a deploy fails if the container has none or never becomes healthy.
- Keep the service on the external `web` network and never publish ports. Docker's published ports bypass ufw.
- Give it a `mem_limit`.
- Get secrets from `env_file: .env` with `required: false`.

`ship config edit <app>` opens this file in your editor, creating it if missing. The TUI's app action **Edit compose.yaml** (`f`) does the same. ship validates it before saving. See [Compose files](../reference/compose.md).

For a `github-release` app, also add `apps/<name>/Dockerfile`. It is uploaded with the compose file and the release binary is copied in as `BINARY`.

## 3. Add data and backups

If the app has data, set `uid`, `data` and a `backup` spec, and point the volume at the data path:

```yaml
uid: 10001
data: [/srv/hello]
backup:
  sqlite: [/srv/hello/hello.db]
```

Use `files` for directories and `postgres` for a compose service. See [Back up and restore](back-up-and-restore.md).

## 4. Add commands if needed

```yaml
build:
  commands: |
    npm ci --audit false
    npm run build
deploy:
  commands: |
    php artisan migrate --force
```

`build.commands` run on your machine before an image build; `deploy.commands` run on the server in the new image just before it goes live. See [Build and deploy commands](build-and-deploy-commands.md).

## 5. Deploy

```sh
ship prod proxy update
ship prod deploy hello
```

`proxy update` is needed when the app's domains are new or changed. Then [deploy](deploy-an-app.md).

## 6. Put secrets on the server

Keep secrets out of `ship.yaml`. Put them in `/opt/apps/<name>/.env`:

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

[`examples/`](../../examples) has one app of each type:

- `hello`, a GitHub release app with SQLite data.
- `web`, an image app built from a local repository.
