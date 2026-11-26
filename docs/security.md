# Security

- **Verified releases.** App release binaries are checked against the release's `SHA256SUMS` or `checksums.txt`, and the built image must report the tag.
- **Signed updates.** `ship update` installs only releases whose `SHA256SUMS` carries a valid Ed25519 signature bound to the version. See [Updates](updates.md).
- **No registry.** Image apps go straight from your machine to the server over SSH, so there are no registry tokens to leak.
- **Backups before changes.** A backup is taken before every switch (`pre-<tag>`) and before every restore (`pre-restore`).
- **Health-checked switches.** A switch succeeds only when compose reports healthy and the optional http check passes. Otherwise the previous image is restored, and ship says that migrations were not undone.
- **One open door.** Only Caddy publishes ports; apps live on the external `web` network. ufw allows SSH, 80 and 443.
- **Validated proxy config.** The Caddyfile is validated in a container before the old one is replaced, and restored on failure.
- **No shell injection.** Every argument is validated in the CLI and again in the agent. Shell arguments are quoted, never evaluated.
- **Secrets stay on the server.** They are read from stdin, stored only on the server and in backups, and never printed or put on a command line.
- **Least privilege for CI.** The `deploy` user may only run `ship-agent deploy`. See [Deploying from CI](deploying.md#deploying-from-ci).
- **Locked-down SSH.** `ship <server> lock-ssh` turns off root and password logins.
- **Confirmations.** `--dry-run` prints what would run, and destructive actions need confirmation or `--yes`.
