# Adding an app

1. Add the app under its server's `apps:` in `ship.yaml`, or with `a` on the server page in the [TUI](tui.md). See [Configuration](configuration.md).
2. Write `apps/<name>/compose.yaml`:
   - The primary service name must match the `upstream` host.
   - Use `image: ${IMAGE}`.
   - Keep the service on the external `web` network and never publish ports: Docker's published ports bypass ufw.
   - Give it a healthcheck and a `mem_limit`.
3. If it has data, set `uid`, `data` and a `backup` spec, and point the volume at the data path.
4. Add `build.commands` and `deploy.commands` if the app needs them.
5. `ship <server> deploy <name>`.
6. Put secrets in `/opt/apps/<name>/.env` with `ship <server> env edit <name>`, not in `ship.yaml`, then `ship <server> restart <name>`.

[`examples/`](../examples) has one app of each type: `hello`, a GitHub release app with SQLite data, and `web`, an image app.

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
