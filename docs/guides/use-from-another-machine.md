# Use ship from another machine

The server is the source of truth. Every server keeps the desired and running configuration for its apps, so a new machine needs only how to reach the server (and local checkouts for image builds). There is nothing to import.

The new machine needs its own SSH key on the server first; see [Add another machine or person](ssh-access.md#add-another-machine-or-person).

## Add the server

```yaml
servers:
  prod:
    host: ops@203.0.113.10
    identity_file: ~/.ssh/prod
```

That is the whole local entry. Apps and server settings (`admin_user`, `caddy_image`) are read from the server, not listed in `ship.yaml`. Run:

```sh
ship prod status
ship prod diff
```

Both read the server's desired and running config. Use `ship prod app edit <app>` or `ship prod config edit <app> compose` to change it from this machine. The next deploy applies record, compose and Dockerfile changes; domain and redirect changes need `ship prod proxy restart`.

## What's stored

| Path | What |
|---|---|
| `/opt/apps/<app>/.ship/desired/ship-app.yaml` | desired app record; authoritative |
| `/opt/apps/<app>/.ship/desired/compose.yaml` | desired compose file |
| `/opt/apps/<app>/.ship/desired/Dockerfile` | desired Dockerfile, release apps only |
| `/opt/apps/.ship/server.yaml` | desired `admin_user` and `caddy_image` |
| `/opt/apps/<app>/ship-app.yaml` | running record from the last successful deploy |
| `/opt/apps/<app>/compose.yaml` | running compose file |
| `/opt/apps/ship.yaml` | generated, read-only list of last-deployed records |

A deploy reads the desired configuration from the server, sends only the app name and version (plus the image for an image app), and promotes desired to running when the app is healthy. Secrets in `/opt/apps/<app>/.env` are never part of the record or of config sync.

## Image apps without a local source

An image app's record never stores a machine-specific checkout path. Deploying a version already on the server, rolling back, backing up, restoring and comparing all work without one. To build a new version, point the local `sources` entry at your checkout:

```sh
ship prod app set web source.path ~/code/web
```

This edits only this machine's `servers.prod.sources.web`. Use an empty value to clear it:

```sh
ship prod app set web source.path ""
```

## See differences

```sh
ship prod status            # config=applied, pending deploy or not deployed
ship prod diff              # desired vs running, proxy and setup state
ship prod diff blog         # one app's config diff
```

A successful deploy makes the app `applied`. A desired change that has not been deployed shows as `pending deploy`; an app that has never run shows as `not deployed`.

If the proxy is out of sync, `diff` and `status` say so. Apply routing changes with:

```sh
ship prod proxy restart
```

There is no `import` command. If this machine still has an old full-format `ship.yaml`, migrate that file once with [config push](edit-config.md#migrate-an-old-local-file).

## Related

- [Edit config](edit-config.md)
- [Deploy an app](deploy-an-app.md)
- [ship.yaml](../reference/ship-yaml.md#keys)
