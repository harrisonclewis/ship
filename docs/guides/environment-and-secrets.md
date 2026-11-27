# Environment values and secrets

There are two places for environment values:

| Place | Use for | How it reaches the app |
|---|---|---|
| `env:` in the desired app record | non-secret values | written to `.ship-env` and `.ship.env`, available to compose as `${VAR}` |
| `/opt/apps/<app>/.env` | secrets | read by the container through `env_file` |

## Non-secret values

`env` is part of the app's desired record on the server:

```yaml
env:
  HELLO_DB: /data/hello.db
  LOG_LEVEL: info
```

Edit it with:

```sh
ship prod app edit hello
```

There is no `app set` key for `env`; use `app edit`.

Saving writes the values to `/opt/apps/<app>/.ship/desired/.ship-env`. A deploy writes them plus `IMAGE` to `/opt/apps/<app>/.ship.env`, and compose reads that file for `${VAR}` interpolation. `IMAGE` is reserved for ship.

Values may use only letters, digits and `_./:@,+*%~=?-` (no spaces, quotes, `$` or `#`), up to 4096 characters. Put anything else, and every secret, in `.env`.

Deploy to apply the change:

```sh
ship prod deploy hello
```

A restart uses the running environment, so it does not pick up a desired `env` change that has not been deployed.

## Secrets

Edit the app's secret file on the server:

```sh
ship prod env edit hello
```

This runs `sudoedit` on `/opt/apps/hello/.env`, creating it with mode 0600 if it does not exist.

After changing secrets, restart the app to pick them up:

```sh
ship prod restart hello
```

Reference the file from compose:

```yaml
services:
  hello:
    env_file:
      - path: .env
        required: false
```

Secrets in `.env` are read by the container through `env_file`; they are not available for `${VAR}` compose interpolation. They stay on the server, are included in backups, and are never printed or put on a command line.

See [ship.yaml](../reference/ship-yaml.md#app-record-keys) and [Compose files](../reference/compose.md).
