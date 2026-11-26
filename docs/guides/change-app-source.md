# Change the app source

`source.type` is either `github-release` or `image`.

| Type | Where the image comes from | Keys |
|---|---|---|
| `github-release` | built on the server from a release binary | `repo`, optional `asset` |
| `image` | built on your machine from a local git checkout and sent over SSH | `path` |

## GitHub release app

```yaml
source:
  type: github-release
  repo: example/hello
  asset: hello_linux_{arch}
```

- `repo` is `owner/repo`.
- `asset` defaults to `{app}_linux_{arch}` and must contain `{arch}`. `{app}` and `{arch}` are expanded; `{arch}` is `amd64` or `arm64` from the server's `arch`.
- `source.path` is not allowed.

Change it locally:

```sh
ship prod app set hello source.repo example/hello
ship prod app set hello source.asset hello_linux_{arch}
```

## Image app

```yaml
source:
  type: image
  path: ~/code/web
```

- `path` is a local git checkout with a `Dockerfile`, absolute or starting with `~/`.
- `repo` and `asset` are not allowed.
- If `path` is empty, the app can still be deployed to an existing version, rolled back, backed up, restored and compared, but it cannot be built.

Change it locally:

```sh
ship prod app set web source.path ~/code/web
```

Use `ship config edit` or the TUI's **All settings** (`a`) to change `source.type` itself. **Edit…** (`i`) → **Source** (`s`) changes the repository/asset for a release app, or the path for an image app.

## How the version is chosen

- Release apps: `latest`, or the tag you pass to `ship prod deploy <app> <tag>`. Tags match `vX.Y.Z[-suffix]`.
- Image apps: `git describe --tags --always` in `source.path`. A dirty tree refuses to build unless you pass `--allow-dirty`, which appends `-dirty-<UTC timestamp>`.

See [Deploy an app](deploy-an-app.md) and [Build and deploy commands](build-and-deploy-commands.md).
