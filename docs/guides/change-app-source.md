# Change the app source

`source.type` is either `github-release` or `image`. The type, repository and asset pattern are part of the desired app record on the server. The local checkout path for an image app is the only source setting that stays on this machine.

| Type | Where the image comes from | Record keys | Local key |
|---|---|---|---|
| `github-release` | built on the server from a release binary | `repo`, optional `asset` | none |
| `image` | built on your machine from a local git checkout and sent over SSH | `type: image` | `servers.<name>.sources.<app>` |

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

The build checkout is local:

```yaml
servers:
  prod:
    host: ops@203.0.113.10
    sources:
      web: ~/code/web
```

- `sources.web` is a local git checkout with a `Dockerfile`, absolute or starting with `~/`.
- It is never uploaded and never appears in the server record.
- If it is empty, the app can still be deployed to an existing version, rolled back, backed up, restored and compared, but it cannot be built.

Set it locally:

```sh
ship prod app set web source.path ~/code/web
```

This edits only `servers.prod.sources.web`. Clear it with an empty value:

```sh
ship prod app set web source.path ""
```

Change the type itself on the server, then deploy:

```sh
ship prod app set web source.type image
ship prod deploy web
```

`app set ... source.type image` clears `repo` and `asset`. Changing to `github-release` needs a `repo` before the record validates.

## How the version is chosen

- Release apps: `latest`, or the tag you pass to `ship prod deploy <app> <tag>`. Tags match `vX.Y.Z[-suffix]`.
- Image apps: `git describe --tags --always` in `sources.<app>`. A dirty tree refuses to build unless you pass `--allow-dirty`, which appends `-dirty-<UTC timestamp>`.

The build platform is the server's detected architecture, `linux/amd64` or `linux/arm64`.

See [Deploy an app](deploy-an-app.md), [Build and deploy commands](build-and-deploy-commands.md), and [ship.yaml](../reference/ship-yaml.md#app-record-keys).
