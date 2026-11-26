# Build and deploy commands

Two optional scripts cover most app needs:

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

Both are YAML block scalars and run with `sh -e`, stopping at the first failing line. Blank lines and `#` comments are fine. Scripts are limited to 256 KiB and must use LF line endings with no NUL characters.

## Build commands

`build.commands` run on your machine in `source.path`, after the clean-tree check and before `docker buildx build`. They are only valid for `source.type: image`.

- They run as one `sh` process. On Windows that means installing [Git for Windows](https://git-scm.com/download/win).
- A failure stops the deploy before anything reaches the server.
- Your `Dockerfile` copies in what they produce.
- They do not run when you deploy a version that is already loaded on the server.

## Deploy commands

`deploy.commands` run on the server in a one-off container of the new image for the `upstream` service. The container gets the app's environment, volumes and network. The entrypoint is replaced by `sh`, so the image must contain `sh`.

- They run after the pre-deploy backup and before the switch.
- They are uploaded as `.ship-deploy.sh`, never put on a command line.
- If they fail, ship switches back to the previous image and leaves the maintenance page on, because the data may already have changed.
- A rollback does not run them.

## Change the commands

```sh
ship prod app set web build.commands - < build.sh
ship prod app set web deploy.commands - < deploy.sh
ship prod app set web deploy.maintenance false
```

A value of `-` reads the script from stdin. An empty value clears it. In the TUI, choose **Edit…** (`i`) → **Build & deploy commands** (`c`).

See [Deploy an app](deploy-an-app.md) for where the commands fit in a deploy, and [ship.yaml](../reference/ship-yaml.md#keys) for the keys.
