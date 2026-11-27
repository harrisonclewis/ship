# Compose files reference

Each app has a hand-written compose file in its desired configuration on the server:

```text
/opt/apps/<app>/.ship/desired/compose.yaml
```

Edit it with `ship <server> config edit <app> compose`; a deploy reads it from there. There is no local `apps/<name>/compose.yaml`.

```yaml
services:
  hello:
    image: ${IMAGE}
    restart: unless-stopped
    networks: [web]
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/health"]
      interval: 10s

networks:
  web:
    external: true
```

## Requirements

| Rule | Why |
|---|---|
| At least one service uses `image: ${IMAGE…}` | ship writes the app's deploy image to `/opt/apps/<name>/.ship.env` as `IMAGE` |
| The service named in `upstream` listens on that port | Caddy proxies to it, and deploy commands and health checks use it |
| The `upstream` service on the external `web` network | Caddy can only reach services there; Docker's published ports bypass ufw |
| A `healthcheck` | a deploy fails if the container has none or never becomes healthy |
| No `ports:` | apps are reached through Caddy only |
| A `mem_limit` | keeps one app from consuming the server |

ship validates that the file decodes, has services, and has a service using `${IMAGE` before it saves the desired file. The rest is enforced when the app deploys.

## Server-side files

A deploy sends only the app name and version, plus the image for an image app. The app's files are already part of its configuration on the server.

| Desired file | Source |
|---|---|
| `ship-app.yaml` | the app record, edited with `app create`, `app edit` or `app set` |
| `compose.yaml` | edited with `config edit <app> compose` |
| `Dockerfile` | edited with `config edit <app> Dockerfile`, release apps only |
| `.ship-meta`, `.ship-env`, `.ship-deploy.sh` | generated from the record when it is saved |

A successful deploy promotes these to the running files at `/opt/apps/<app>/`. The running `compose.yaml`, `Dockerfile`, `ship-app.yaml`, `.ship-meta`, `.ship-env` and `.ship-deploy.sh` are what the app last deployed with. `restart` and backup use the running files; a new deploy reads desired config.

## Environment

- `${VAR}` interpolation reads `/opt/apps/<name>/.ship.env`, which holds `IMAGE` plus the app's non-secret `env` values.
- Secrets live in `/opt/apps/<name>/.env` and reach the container through `env_file`, not through `${VAR}` interpolation.
- Use `required: false` so a missing `.env` doesn't stop a first deploy:

```yaml
    env_file:
      - path: .env
        required: false
```

## Editing

```sh
ship prod config edit <app> compose
ship prod config edit <app> Dockerfile
```

ship fetches the desired file to a temporary copy, opens it in `$VISUAL`, `$EDITOR` or `vi`, validates compose files and puts the result back. A missing compose file opens empty; a missing Dockerfile can be created the same way. Other changes on the server are detected and the save is refused.

Deploy to apply the change:

```sh
ship prod deploy <app>
```

`app create` seeds a minimal compose file, so you can create an app and edit its compose file straight away.

See [Add an app](../guides/add-an-app.md) for the full checklist, and [ship.yaml](ship-yaml.md) for the app record keys.
