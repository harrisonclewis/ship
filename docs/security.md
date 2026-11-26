# Security

- **Verified releases.** App release binaries are checked against the release's `SHA256SUMS` or `checksums.txt`, and the built image must report the tag. See [Deploy an app](guides/deploy-an-app.md#github-release-apps).
- **Signed updates.** `ship update` installs only releases whose `SHA256SUMS` carries a valid Ed25519 signature bound to the version. See [Update ship](guides/update-ship.md).
- **No registry.** Image apps go straight from your machine to the server over SSH, so there are no registry tokens to leak.
- **Backups before changes.** A backup is taken before a switch when the app has data on the server, and before every restore (`pre-restore`).
- **Health-checked switches.** A switch succeeds only when compose reports healthy and the optional http check passes. Otherwise the previous image is restored, and ship says that migrations were not undone.
- **One open door.** Only Caddy publishes ports; apps live on the external `web` network. ufw allows SSH, 80 and 443.
- **Validated proxy config.** The Caddyfile is validated in a container before the old one is replaced, and restored on failure.
- **No shell injection.** Every argument is validated in the CLI and again in the agent. Shell arguments are quoted, never evaluated.
- **Secrets stay on the server.** They are edited with `sudoedit` over ssh, stored only on the server and in backups, and never printed or put on a command line.
- **Least privilege for CI.** The `deploy` user may only run `ship-agent deploy`. See [Deploy from CI](guides/deploy-from-ci.md).
- **Locked-down SSH.** `ship <server> lock-ssh` turns off root and password logins. See [Add a server](guides/add-a-server.md#4-lock-ssh-down).
- **Confirmations.** `--dry-run` prints what would run, and destructive actions need confirmation or `--yes`.
