# ship.yaml reference

`ship.yaml` lists your servers and the apps on each one. It is validated strictly before anything reaches a server, because its values end up on a shell line there. Unknown keys, a second YAML document and invalid values are rejected.

Start from [`examples/`](../../examples), or add servers and apps from the [TUI](tui.md).

## Where ship looks

1. `SHIP_CONFIG`, if set. A missing file is an error.
2. `./ship.yaml` in the current directory.
3. `~/.config/ship/ship.yaml` (`$XDG_CONFIG_HOME/ship` on Linux when set, `%USERPROFILE%\.config\ship` on Windows).

Each app's `apps/<name>/` folder is read from beside the `ship.yaml` that was found. Apps with the same name on different servers share that folder.

## Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `backup_dir_local` | string | `~/backups/vps` | where `ship <server> backup` downloads archives, under `<server>/<app>/` |
| `servers` | map | — | server name to server |
| `servers.<name>.host` | string | required | ssh destination, `user@host` (`root@…` only for the first setup) |
| `servers.<name>.identity_file` | string | empty | optional SSH private key for this server, e.g. `~/.ssh/prod`; ssh offers only that key. Without it, ship uses your ssh config and agent |
| `servers.<name>.admin_user` | string | `ops` | sudo user created by `setup`; not `root` or `deploy` |
| `servers.<name>.arch` | string | `linux/amd64` | `linux/amd64` or `linux/arm64`; also selects the release asset |
| `servers.<name>.caddy_image` | string | `caddy:2.10-alpine` | pinned Caddy image; a tag or sha256 digest is required |
| `servers.<name>.apps` | map | empty | app name to app |
| `apps.<name>.domains` | list | required | one or more FQDNs, all served; the first is canonical |
| `apps.<name>.redirects` | list | empty | optional FQDNs that permanently redirect to the canonical domain |
| `apps.<name>.upstream` | string | required | `service:port` on the `web` network, e.g. `hello:8080` |
| `apps.<name>.source.type` | string | required | `github-release` or `image` |
| `apps.<name>.source.repo` | string | — | `owner/repo`; required for `github-release`, not allowed for `image` |
| `apps.<name>.source.asset` | string | `{app}_linux_{arch}` | release asset pattern; must contain `{arch}`; `github-release` only |
| `apps.<name>.source.path` | string | empty | local git checkout for an image app, absolute or `~/…`; `image` only |
| `apps.<name>.uid` | integer | empty | optional system user/group and data owner; required for SQLite backups |
| `apps.<name>.data` | list | empty | server directories the app owns |
| `apps.<name>.health.http_path` | string | empty | optional extra path checked from inside `web` after a switch |
| `apps.<name>.backup.sqlite` | list | empty | SQLite database files |
| `apps.<name>.backup.files` | list | empty | files or directories copied with owners and modes |
| `apps.<name>.backup.postgres.service` | string | — | compose service running Postgres |
| `apps.<name>.backup.postgres.db` | string | — | database name |
| `apps.<name>.backup.postgres.user` | string | — | database user |
| `apps.<name>.env` | map | empty | non-secret values; uploaded and available to compose as `${VAR}` |
| `apps.<name>.build.commands` | string | empty | image apps only: a script run on your machine in `source.path` before the image is built |
| `apps.<name>.deploy.commands` | string | empty | a script run on the server in the new image just before it goes live |
| `apps.<name>.deploy.maintenance` | boolean | `true` | show a 503 maintenance page while deploying |

`servers.<name>.apps.<app>` is the full app block; the table writes its children as `apps.<name>…` for brevity.

The old flat format is no longer supported: move top-level `host`, `admin_user`, `arch`, `caddy_image` and `apps` under `servers.<name>`.

## Shape

```yaml
backup_dir_local: ~/backups/vps

servers:
  prod:
    host: ops@203.0.113.10
    identity_file: ~/.ssh/prod
    admin_user: ops
    arch: linux/amd64
    caddy_image: caddy:2.10-alpine
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
        deploy:
          commands: |
            npm run migrate
          maintenance: true
      web:
        domains: [example.com]
        upstream: web:3000
        source:
          type: image
          path: ~/code/web
        build:
          commands: |
            npm ci --audit false
            npm run build
```

## Rules

- App and server names are 2–32 characters: lowercase letters, digits and dashes, starting with a letter and ending with a letter or digit.
- `all` and `proxy` are reserved app names. Server names can't be a command (`setup`, `deploy`, `status`, `config`, …) or `all`.
- `domains` and `redirects` must be lowercase FQDNs with at least one dot and no trailing dot.
- Every domain and redirect must be unique across every app on every server.
- Server paths must be clean and absolute, use only letters, digits and `._/-`, and not be `/`.
- `source.path` must be a clean absolute path or start with `~/`.
- `uid` is between 1 and 2147483647.
- Env names match `[A-Za-z_][A-Za-z0-9_]*`; `IMAGE` is reserved. Values may use only letters, digits and `_./:@,+*%~=?-`, up to 4096 characters. Put anything else, and every secret, in the server's `.env` with `ship <server> env edit <app>`.
- Scripts must use LF line endings, contain no NUL characters, and be at most 256 KiB.
- `health.http_path` starts with `/` and has no spaces, quotes or backslashes.
- `caddy_image` must pin a tag or a sha256 digest.
- `build.commands` is valid only for `source.type: image`.

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

See [Build and deploy commands](../guides/build-and-deploy-commands.md) for when each runs.

Quick changes to source, domains, redirects, build/deploy commands and the maintenance flag can be made with `ship <server> app set|add|remove`; see [Edit config](../guides/edit-config.md).

## Compose files

Each app has a hand-written `apps/<name>/compose.yaml`. `ship config edit <name>` or the TUI app action opens it in your editor, creating it if missing; ship validates it and keeps a `.bak` before saving. ship uploads it, plus `apps/<name>/Dockerfile` for release apps.

See [Compose files](compose.md).
