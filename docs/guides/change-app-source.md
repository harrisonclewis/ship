# Change the app source

`source.type` is either `github-release` or `image`. The type, repository and asset pattern are part of the desired app record on the server. The local directory or Git repository URL for an image app is the only source setting that stays on this machine.

| Type | Where the image comes from | Record keys | Local key |
|---|---|---|---|
| `github-release` | built on the server from a release binary | `repo`, optional `asset` | none |
| `image` | built on your machine from a local directory or Git URL and sent over SSH | `type: image` | `servers.<name>.sources.<app>` |

The TUI offers three source choices: **Source directory**, **Git repository URL** and **GitHub release asset**. The first two are both `source.type: image`; only the local `sources.<app>` value differs. The same choice is available when editing an existing image app's source.

## Change the source in the TUI

Open the app, choose **Edit…** (`i`) and **Source** (`s`). The first step shows the saved source kind and its current values: the directory, the Git URL with its build directory, or the release repository with its asset. The selector defaults to the saved kind. Press `Enter` to edit the details, which are already populated.

- `Esc` in the details step returns to the kind step and keeps your draft.
- `Esc` on the kind step returns to the Edit menu without saving.
- Nothing is saved until you submit the details step.

The screen re-reads the local `ship.yaml` when it opens, so edits made outside the TUI are reflected. If the file cannot be read, Ship shows the error instead of opening fields from stale values.

A missing directory or source is labelled as not set, and optional values that use a default — the Git build directory or release asset pattern — say so explicitly.

## GitHub release app

```yaml
source:
  type: github-release
  repo: example/hello
  asset: hello_linux_{arch}
```

- `repo` is `owner/repo`.
- `asset` defaults to `{app}_linux_{arch}` and must contain `{arch}`. `{app}` and `{arch}` are expanded; `{arch}` is `amd64` or `arm64`, detected from `uname -m` on the server.
- A local `sources` entry is not used.

Change the record on the server:

```sh
ship prod app set hello source.repo example/hello
ship prod app set hello source.asset hello_linux_{arch}
```

Deploy to apply:

```sh
ship prod deploy hello
```

## Image app

The desired record says only that the app is an image:

```yaml
source:
  type: image
```

The build input is local and may be a directory or a Git URL:

```yaml
servers:
  prod:
    host: ops@203.0.113.10
    sources:
      web: ~/code/web
      api: https://github.com/acme/api.git
      worker: git@github.com:acme/worker.git
    build_dirs:
      api: docs
```

- A directory must be a clean absolute path or start with `~/`.
- A Git URL must use `https://`, `ssh://` or scp-style `git@host:path`. Credentials, query strings, fragments, `file://`, `ext::` and unsafe path segments are rejected.
- `build_dirs` is optional and only for a Git URL. It is a relative directory inside the checkout where build commands, the Dockerfile and the Docker context live. Blank or missing means the repository root.
- The URL is never uploaded and never appears in the server record. A server record cannot change this machine's build input or make Git use local credentials.
- If it is empty, the app can still be deployed to an existing version, rolled back, backed up, restored and compared, but it cannot be built.

Set it locally:

```sh
ship prod app set web source.path ~/code/web
ship prod app set api source.path https://github.com/acme/api.git
```

This updates `servers.prod.sources.<app>` in the local config. Switching to a local directory clears any Git build directory. Clear the source with an empty value:

```sh
ship prod app set web source.path ""
```

For a Git URL, set an optional relative build directory with:

```sh
ship prod app set api source.build_dir docs
ship prod app set api source.build_dir ""
```

A non-empty `source.build_dir` requires a Git URL and a relative path inside the checkout. Ship rejects `..` components and symlinks that escape the checkout.

Change the type itself on the server, then deploy:

```sh
ship prod app set web source.type image
ship prod deploy web
```

`app set ... source.type image` clears `repo` and `asset`. Changing to `github-release` needs a `repo` before the record validates.

### Git URL builds

Ship runs every Git command non-interactively: ssh is given `BatchMode=yes`, and an existing `GIT_SSH_COMMAND` or `core.sshCommand` is kept and extended. A bare `GIT_SSH` is left alone. Ship never accepts a host key automatically and never writes to `known_hosts`. If the host key is unknown, the error names the host and shows the check to run (`ssh -T git@<host>`, including the user and port from the source URL); trust it once in a terminal, then retry. If the host key has **changed**, Ship refuses to offer that step: check the key manually first. An `https://` URL avoids SSH keys entirely.

A Git URL is fetched into `~/.ship/servers/<server>/repositories/<host>/...`, keyed by the full host and repository path so two repositories with the same basename do not collide. The cache origin is checked before every use. Each build clones the resolved commit into a separate directory under `~/.ship/servers/<server>/builds/<app>/`; a failed or concurrent build cannot change another build's or the cache's inputs. Builds use the server's architecture, just like a directory build.

A `build_dirs.<app>` setting changes the build directory inside that isolated checkout. Build commands run there, the Dockerfile or `Dockerfile.release` is selected there, and Docker uses it as the context. The complete checkout is still present and is removed after the build. Blank means the repository root.

- No version: the remote default branch's latest commit.
- `commit:<sha>`: build that commit (7–40 hex characters; ambiguous short SHAs are refused).
- `release:<tag>`: verify the tag is a published GitHub release through `gh`, then build source at the tag's commit.
- `release:<tag>:<asset>`: use one asset from that published release instead of building source. See below.

The build image is tagged with the full commit SHA. When a non-root build directory is set, a stable `-dir-<hash>` suffix is added so changing the directory at the same commit builds a new image. The TUI's **v** action opens a Commits/Releases picker; releases offer **Build source** or a downloadable asset. The same selection is available on the CLI:

```sh
ship prod deploy api                       # default branch HEAD
ship prod deploy api commit:9f2c1ab        # a commit
ship prod deploy api release:v1.2.3        # source at the release tag
ship prod deploy api release:v1.2.3:api_linux_amd64
```

The last successfully deployed identity is stored in `/opt/apps/<app>/.ship-state` as `current`/`previous` (image tags) plus `current_ref`/`previous_ref` (`branch:main`, `commit:<sha>`, `release:<tag>` or `release:<tag>:<asset>`). Rollback and a failed deploy restore the matching refs. `status` marks `newer=yes` when the running commit is behind the remote default branch, or when a newer published release exists for a release identity. Network and authentication failures never count as newer.

### Release assets from a Git URL

The asset route uses the trusted repository checkout at the release tag as the Docker build context, or the `build_dirs.<app>` directory inside it. It requires `Dockerfile.release`, or a root `Dockerfile` that declares `ARG BINARY`; the asset is copied into the context as `ship-release-binary` and passed as `BINARY`, with the tag as `VERSION`. The Dockerfile must consume `BINARY` (and unpack an archive if needed). Source `build.commands` are not run for this route. Release assets are downloaded with `gh api` on this machine, verified against GitHub's asset digest or the release's `SHA256SUMS`/`checksums.txt`, and staged outside the checkout before being copied in. Checksum files are not offered as choices. The image version includes the tag, asset name, source commit and verified payload digest, plus the build directory when set, so replacing an asset, moving a tag, or changing the build directory produces a distinct image and cannot overwrite the rollback tag.

## How the version is chosen

- Release apps: `latest`, or the tag you pass to `ship prod deploy <app> <tag>`. Tags match `vX.Y.Z[-suffix]`.
- Directory image apps: `git describe --tags --always` in `sources.<app>`. A dirty tree refuses to build unless you pass `--allow-dirty`, which appends `-dirty-<UTC timestamp>`.
- Git URL image apps: the commit SHA, with a typed ref as described above.

The build platform is the server's detected architecture, `linux/amd64` or `linux/arm64`.

See [Deploy an app](deploy-an-app.md), [Build and deploy commands](build-and-deploy-commands.md), and [ship.yaml](../reference/ship-yaml.md#app-record-keys).
