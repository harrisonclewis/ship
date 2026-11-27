# Build and deploy commands

Two optional scripts cover most app needs. Both are part of the app's desired record on the server:

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

Both are YAML block scalars and run with `sh -e`, stopping at the first failing line. Blank lines and `#` comments are fine. Scripts are limited to 256 KiB and must use LF line endings with no NUL characters. Editing the record does not run them: a deploy runs deploy commands, and runs build commands when it builds an image.

## Build commands

`build.commands` run on your machine in `sources.<app>`, after the clean-tree check and before `docker buildx build`. They are only valid for `source.type: image`.

For a Git URL source they run in the isolated checkout of the resolved commit. When `build_dirs.<app>` is set, that is the working directory inside the checkout, and Docker uses the same directory as its context. A release-asset deploy from a Git URL (`release:<tag>:<asset>`) does not run `build.commands`: it builds from the verified prebuilt asset.

Because the script comes from the server, ship treats it as untrusted on each machine. When it is new or has changed, ship prints it and asks before running it:

```text
The build script for web on prod comes from the server and is not yet approved on this machine:
...
```

- `--yes` approves without a prompt.
- Without a TTY, an unapproved script fails unless you pass `--yes`.
- Approvals are stored in `build-approvals.yaml` under the ship config directory, not in `ship.yaml`.
- `--dry-run` prints the script and does not run the build.

The rest of the behaviour is unchanged:

- They run as one `sh` process. On Windows that means installing [Git for Windows](https://git-scm.com/download/win).
- A failure stops the deploy before anything reaches the server.
- Your `Dockerfile` copies in what they produce.
- They do not run when you deploy a version that is already loaded on the server.

Changing the script later makes the next deploy ask again on each machine. `ship prod app set web build.commands - < build.sh` updates the desired record; `ship prod deploy web` runs it.

## Deploy commands

`deploy.commands` run on the server in a one-off container of the new image for the `upstream` service. The container gets the app's environment, volumes and network. The entrypoint is replaced by `sh`, so the image must contain `sh`.

- They read from the desired record and run after the pre-deploy backup and before the switch.
- They are stored as `.ship-deploy.sh` on the server, never put on a command line.
- If they fail, ship switches back to the previous image and leaves the maintenance page on, because the data may already have changed.
- A rollback does not run them.

## Change the commands

```sh
ship prod app set web build.commands - < build.sh
ship prod app set web deploy.commands - < deploy.sh
ship prod app set web deploy.maintenance false
```

A value of `-` reads the script from stdin. An empty value clears it. You can also edit the record:

```sh
ship prod app edit web
```

Then deploy to apply:

```sh
ship prod deploy web
```

A domain-only change does not need a deploy; apply it with `ship prod proxy restart`.

See [Deploy an app](deploy-an-app.md) for where the commands fit in a deploy, and [ship.yaml](../reference/ship-yaml.md#app-record-keys) for the keys.
