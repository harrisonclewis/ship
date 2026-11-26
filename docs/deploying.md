# Deploying

```sh
ship prod deploy <app> [version]
ship prod deploy all
```

Or select an app on a server page in the [TUI](tui.md) and choose **Deploy**.

## What a deploy does

1. **Build** (image apps): run `build.commands` locally, build the image and send it to the server.
2. **Upload** the app's files in one transfer.
3. **Maintenance on**: Caddy answers every domain and redirect of the app with a 503 page and `Retry-After`.
4. **Back up** the app's data as `pre-<version>`. Nothing writes through the site while the page is up, so the backup is consistent.
5. **Deploy commands**: run `deploy.commands` in a one-off container of the new image.
6. **Switch** with `docker compose up -d --wait`, then check the compose healthcheck and the optional `health.http_path`.
7. **Maintenance off.**

With `deploy.maintenance: false`, steps 3 and 7 are skipped and the site stays up while the new version starts.

Current and previous images are recorded in `.ship-state`; only those two are kept.

## When a deploy fails

| Failed at | ship does |
|---|---|
| proxy check, build, image transfer or upload | the running app is untouched |
| backup or anything before the deploy commands | keeps the previous version and turns maintenance off |
| deploy commands, switch or health checks | switches back to the previous image and **leaves maintenance on** |

The last case is deliberate: the deploy commands may already have changed the database, and the old code may not work with it. ship prints the pre-deploy archive and the recovery commands:

```sh
ship prod restore <app> <archive>
ship prod maintenance off <app>
```

If you know the data is fine, `maintenance off` alone brings the old version back online.

## Build and deploy commands

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

- **Build commands** run on your machine in `source.path`, after the clean-tree check and before `docker buildx build`. They use `sh`; on Windows that means installing [Git for Windows](https://git-scm.com/download/win). A failure stops the deploy before anything reaches the server. Your `Dockerfile` copies in what they produce.
- **Deploy commands** run on the server in a one-off container of the new image for the `upstream` service, with the app's environment, volumes and network. The entrypoint is replaced by `sh`, so the image must contain `sh`. They are uploaded as `.ship-deploy.sh`, never put on a command line. A rollback doesn't run them.

Both run with `sh -e` and stop at the first failing line.

## Maintenance page

```sh
ship prod maintenance on <app>
ship prod maintenance off <app>
```

A flag file in `/opt/proxy/maintenance/` switches the page, so Caddy needs no reload. You can turn it on by hand for manual work, too.

## GitHub release apps

The image is built on the server from a release binary.

1. `ship prod deploy hello` resolves `latest` from GitHub's latest-release redirect, or takes the tag you pass. Tags match `vX.Y.Z[-suffix]`.
2. The agent downloads the asset (`hello_linux_<arch>` here) and the release's `SHA256SUMS` or `checksums.txt`, verifies SHA-256, builds `hello:<tag>` from `apps/hello/Dockerfile`, and checks that `docker run … version` reports the tag.

## Image apps

The image is built on your machine and sent to the server over SSH. No registry is involved.

```sh
ship prod deploy web                  # build, send and deploy; version from git describe --tags --always
ship prod deploy web --allow-dirty    # allow uncommitted changes
ship prod deploy web v1.2.3           # redeploy a version already on the server
```

The first form refuses a dirty tree; `--allow-dirty` appends `-dirty-<UTC timestamp>` to the version. It runs:

```sh
docker buildx build --platform <arch> --tag ship/<app>:<version> --load <path>
docker save ship/<app>:<version> | gzip | ssh … ship-agent image-load <app> <version>
```

ship does the compression itself, so no `gzip` binary is needed. The app repo must contain a `Dockerfile`.

## Rollback

```sh
ship prod rollback <app>
```

Switches to the previous image. The database is not restored; use [restore](backups.md#restore) for that.

## Deploying from CI

The `deploy` user may only run `ship-agent deploy`. Add a CI key to `/var/lib/deploy/.ssh/authorized_keys` with a forced command:

```text
command="sudo /usr/local/sbin/ship-agent deploy \"$SSH_ORIGINAL_COMMAND\"",restrict ssh-ed25519 AAAA… ci
```

CI sends the app and tag as one argument, which the agent splits and validates:

```sh
ssh deploy@HOST 'hello v1.2.3'
```

Store the host key in CI (`ssh-keyscan HOST`) rather than turning off host key checking.

CI uses the deploy commands and maintenance setting from the last `ship` deploy. It can deploy any release of a GitHub release app, but can only redeploy an image app version that's already on the server, because the `deploy` user can't load images.
