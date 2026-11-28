# Deploy an app

```sh
ship prod deploy <app> [version]
ship prod deploy all
```

`deploy all` deploys every desired app in name order, keeps going when one fails, prints a summary and exits non-zero if any failed.

A deploy reads the app's config from the server. It sends only the app name and version, plus the locally built image for an image app. Changes to the record, compose file or Dockerfile are saved as desired config immediately, but they become running config when the deploy succeeds.

Deploying does not render the Caddyfile. Run `ship prod proxy sync` after a new app's first deploy, and `ship prod proxy restart` after changing domains or redirects.

## Desired, applied and pending

```sh
ship prod status <app>     # config=applied, pending deploy or not deployed
ship prod diff <app>       # desired vs running, plus proxy and setup state
```

- `applied`: desired config matches the running record and files.
- `pending deploy`: there is a desired change that the app has not deployed, or the app has no running record.
- `not deployed`: the app has never run.

A successful deploy promotes the desired record, `compose.yaml`, `Dockerfile` and generated files to the running files at `/opt/apps/<app>/`.

If this machine still has an old full-format `ship.yaml`, migrate it once per server with [config push](edit-config.md#migrate-an-old-local-file) before deploying.

## What a deploy does

1. **Read** the desired app config from the server.
2. **Check the proxy** can serve the maintenance page, when the app uses one.
3. **Build** a release app on the server, or an image app on your machine. Build commands run before an image build.
4. **Load** a locally built image on the server.
5. **Maintenance on**: Caddy answers every domain and redirect of the app with a 503 page and `Retry-After`.
6. **Back up** the app's data as `pre-<version>`. Nothing writes through the site while the page is up, so the backup is consistent.
7. **Write the app environment** from the desired record.
8. **Deploy commands**: run `deploy.commands` in a one-off container of the new image.
9. **Switch** with `docker compose up -d --wait`, then check the compose healthcheck and the optional `health.http_path`.
10. **Maintenance off**, then **promote** desired to running.

With `deploy.maintenance: false`, steps 5 and 10 are skipped and the site stays up while the new version starts.

Ship records and retains the current and previous images in `.ship-state`. The file also records the last successful deploy or rollback time in UTC. No-op deploys, failed deploys and restarts keep the existing time.

## When a deploy fails

This table assumes the app uses the maintenance page.

| Failed at | ship does |
|---|---|
| reading desired config, proxy check, build (release or image) or image transfer | the running app is untouched; maintenance is not on yet |
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

The image is built on the server from a release binary, using the desired record and Dockerfile.

1. `ship prod deploy hello` resolves `latest` from GitHub's latest-release redirect, or takes the tag you pass. Tags match `vX.Y.Z[-suffix]`.
2. The agent downloads the asset (`hello_linux_<arch>` here) and the release's `SHA256SUMS` or `checksums.txt`, verifies SHA-256, builds `hello:<tag>` from the desired `Dockerfile`, and checks that `docker run … version` reports the tag.

`{arch}` is detected from `uname -m` on the server: `amd64` or `arm64`. The asset pattern comes from `source.asset` and defaults to `{app}_linux_{arch}`. The Dockerfile receives the downloaded binary as `BINARY` and the tag as `VERSION`.

Deploy a specific release with:

```sh
ship prod deploy hello v1.2.3
```

## Git URL image apps

An image app whose local `sources.<app>` is a Git URL is fetched and built on this machine, then sent to the server like any image app. Its image version is the full commit SHA, plus a stable `-dir-<hash>` suffix when an optional `build_dirs.<app>` directory is set. That directory, relative to the checkout, is where build commands, Dockerfile selection and the Docker build context live; blank means the repository root.

```sh
ship prod deploy web                         # remote default branch HEAD
ship prod deploy web commit:9f2c1ab          # a specific commit
ship prod deploy web release:v1.2.3          # source at a published release tag
ship prod deploy web release:v1.2.3:web_linux_amd64  # a release asset
```

- `release:<tag>` is accepted only when `gh` confirms a published, non-draft GitHub release at that tag.
- `release:<tag>:<asset>` downloads the asset with `gh api`, verifies it, and builds from the tag's checkout. The asset build uses `Dockerfile.release` when present; otherwise the root `Dockerfile` must declare `ARG BINARY`. The asset is copied in as `ship-release-binary` and passed as `BINARY`, with the tag as `VERSION`. The build directory and Docker context follow `build_dirs.<app>`. Source build commands are skipped on this route.
- Checksum assets are not offered in the picker. Both the selected asset and the release checksum files are staged separately from the checkout, and a verified copy is placed into the build input.
- The image version for an asset includes the tag, asset name, source commit, verified payload digest and, when set, the build directory, so a replaced asset, moved tag or changed build directory never collides with the rollback image.
- Git never prompts. An unknown SSH host key fails with an error naming the host and the `ssh -T` command to run; the TUI offers `t` to run it in your terminal. A changed host key is never trusted automatically.

The TUI **v** action opens a Commits/Releases picker. Select a release to choose **Build source** or one of its assets. Caches live in `~/.ship/servers/<server>/repositories/`; each build uses a separate directory under `~/.ship/servers/<server>/builds/<app>/`.

`status` shows the running ref and `newer=yes` when the running commit is behind the remote default branch, or when a newer published release exists for a release identity. It never treats an offline or authentication error as newer, and it does not show a badge before the first successful deploy.

## Image apps

The image is built on your machine and sent to the server over SSH. No registry is involved.

```sh
ship prod deploy web                  # build, send and deploy; version from git describe --tags --always
ship prod deploy web --allow-dirty    # allow uncommitted changes
ship prod deploy web v1.2.3           # redeploy a version already on the server
```

The first form refuses a dirty tree; `--allow-dirty` appends `-dirty-<UTC timestamp>` to the version. It runs:

```sh
docker buildx build --platform <arch> --tag ship/<app>:<version> --load <sources path>
docker save ship/<app>:<version> | gzip | ssh … ship-agent image-load <app> <version>
```

The local checkout is `servers.<name>.sources.<app>` in `ship.yaml`, set with:

```sh
ship prod app set web source.path ~/code/web
```

ship does the compression itself, so no `gzip` binary is needed. The app repo must contain a `Dockerfile`. `build.commands` run in that directory first and need approval on this machine.

Deploying a version name only works when that image is already loaded on the server. To build a new version, deploy without a version.

`restart` and backup use the running files. A deploy is what applies desired record, compose and Dockerfile changes.

See [Change the app source](change-app-source.md), [Build and deploy commands](build-and-deploy-commands.md), and [Manage domains](manage-domains.md).
