# Deploy an app

```sh
ship prod deploy <app> [version]
ship prod deploy all
```

From the TUI, select an app on a server page and choose **Deploy latest/build** (`d`) or **Deploy version…** (`v`).

`deploy all` deploys every app in name order, keeps going when one fails, prints a summary and exits non-zero if any failed.

Deploying does not render the Caddyfile. Run `ship prod proxy update` after adding or changing an app's domains or redirects.

## What a deploy does

1. **Check the proxy** can serve the maintenance page, when the app uses one.
2. **Build** (image apps): run `build.commands` locally, build the image and send it to the server.
3. **Upload** the app's files in one transfer.
4. **Maintenance on**: Caddy answers every domain and redirect of the app with a 503 page and `Retry-After`.
5. **Back up** the app's data as `pre-<version>`. Nothing writes through the site while the page is up, so the backup is consistent.
6. **Deploy commands**: run `deploy.commands` in a one-off container of the new image.
7. **Switch** with `docker compose up -d --wait`, then check the compose healthcheck and the optional `health.http_path`.
8. **Maintenance off.**

With `deploy.maintenance: false`, steps 4 and 8 are skipped and the site stays up while the new version starts.

Current and previous images are recorded in `.ship-state`; only those two are kept.

## When a deploy fails

This table assumes the app uses the maintenance page.

| Failed at | ship does |
|---|---|
| proxy check, image build, image transfer or upload | the running app is untouched; maintenance is not on yet |
| release build on the server | the previous version keeps running; maintenance stays on when enabled |
| backup or writing the app environment | keeps the previous version and turns maintenance off |
| deploy commands | restores the previous image and **leaves maintenance on** |
| switch or health check, before any deploy commands | restores the previous image and turns maintenance off |
| switch or health check, after deploy commands ran | restores the previous image and **leaves maintenance on** |

Leaving maintenance on after the deploy commands is deliberate: they may already have changed the database, and the old code may not work with it. ship prints the pre-deploy archive and the recovery commands:

```sh
ship prod restore <app> <archive>
ship prod maintenance off <app>
```

If you know the data is fine, `maintenance off` alone brings the old version back online.

## GitHub release apps

The image is built on the server from a release binary.

1. `ship prod deploy hello` resolves `latest` from GitHub's latest-release redirect, or takes the tag you pass. Tags match `vX.Y.Z[-suffix]`.
2. The agent downloads the asset (`hello_linux_<arch>` here) and the release's `SHA256SUMS` or `checksums.txt`, verifies SHA-256, builds `hello:<tag>` from `apps/hello/Dockerfile`, and checks that `docker run … version` reports the tag.

The asset pattern comes from `source.asset` and defaults to `{app}_linux_{arch}`. The Dockerfile receives the downloaded binary as `BINARY` and the tag as `VERSION`.

Deploy a specific release with:

```sh
ship prod deploy hello v1.2.3
```

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

ship does the compression itself, so no `gzip` binary is needed. The app repo must contain a `Dockerfile`, and `source.path` must point at it. `build.commands` run in that directory first.

Deploying a version name only works when that image is already loaded on the server. To build a new version, deploy without a version.

See [Change the app source](change-app-source.md) and [Build and deploy commands](build-and-deploy-commands.md).
