# Update ship

Installed copies of ship check for a newer release at most once a day. When one is out, ship prints one line telling you to update. The check runs in the background and never slows a command down.

```sh
ship update          # download, verify and install the latest release
ship update --check  # only check
```

`ship update` installs a release only after the Ed25519 signature over its `SHA256SUMS` and the binary's SHA-256 both match. The signature covers the version number, so an older release can't be passed off as a newer one.

- If ship is installed somewhere you can't write to, it says so: run `sudo ship update` on Linux or macOS, or use an elevated terminal on Windows.
- On Windows the running `ship.exe` is renamed to `ship.exe.old` and removed the next time ship starts.
- Set `SHIP_NO_UPDATE_CHECK=1` to turn the daily check off.
- A source build (`make build`) never updates itself; use `git pull`.

## After upgrading ship

Maintenance pages need a mount in the Caddy container that older setups don't have. Run this once per server:

```sh
ship prod setup
ship prod proxy update
```

Until then, deploys of apps with maintenance turned on stop early and say so. See [Maintenance mode](maintenance-mode.md).

## The server agent

`ship-agent` is embedded in the CLI and updated on the server automatically whenever it differs. It carries a protocol version: an older `ship` never overwrites a newer agent, and tells you to run `ship update` instead. That matters when you run ship from more than one machine.

See [Security](../security.md) and [Releasing](../releasing.md).
