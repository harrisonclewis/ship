# Compose files reference

Each app has a hand-written `apps/<name>/compose.yaml` beside `ship.yaml`. Apps with the same name on different servers share that folder.

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
| At least one service uses `image: ${IMAGE…}` | ship writes the app's image to `/opt/apps/<name>/.ship.env` as `IMAGE` |
| The service named in `upstream` listens on that port | Caddy proxies to it, and deploy commands and health checks use it |
| The `upstream` service on the external `web` network | Caddy can only reach services there; Docker's published ports bypass ufw |
| A `healthcheck` | a deploy fails if the container has none or never becomes healthy |
| No `ports:` | apps are reached through Caddy only |
| A `mem_limit` | keeps one app from consuming the server |

ship validates that the file decodes, has services, and has a service using `${IMAGE` before it saves or uploads it. The rest is enforced when the app deploys.

## Files ship uploads

For every deploy, ship uploads only:

| File | Source |
|---|---|
| `compose.yaml` | `apps/<name>/compose.yaml` |
| `ship-app.yaml` | generated from the app's `ship.yaml` block |
| `.ship-meta` | generated app metadata |
| `.ship-env` | the app's non-secret `env` values |
| `.ship-deploy.sh` | `deploy.commands` |
| `Dockerfile` | `apps/<name>/Dockerfile`, release apps only |

Other files in `apps/<name>/` are not uploaded. For image apps the Docker build context is `source.path`, not this folder; that checkout must contain its own `Dockerfile`.

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
ship config edit <app>
```

or **Edit compose.yaml** (`f`) in the TUI. A missing file opens empty and is created on save. ship checks the compose rules and keeps the previous file as `.bak`.

See [Add an app](../guides/add-an-app.md) for the full checklist, and [ship.yaml](ship-yaml.md) for app keys.
