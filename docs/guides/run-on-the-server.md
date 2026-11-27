# Run ship on the server

You can run ship on the VPS it manages. The same commands and TUI work, but ship runs the agent directly on this machine instead of opening an SSH connection. This is useful on a server with no inbound SSH from where you work, or when you are already logged in.

## Add a local server

Use `connection: local` in `ship.yaml`:

```yaml
servers:
  self:
    connection: local
    user: ops                  # optional
    sources:
      web: /srv/code/web       # optional, image apps built on this server
```

- `host` and `identity_file` are not allowed with `connection: local`.
- `user` is optional. It is the admin user ship acts as, for example `ops`. Leave it empty to use the user running ship.
- There is no password setting. ship never reads or stores a password.
- `sources` still points at local checkouts for image apps, now on this machine.

`connection: remote` is the default, so existing remote entries do not need to change. A server with no `connection` keeps using SSH.

## Run ship

Install ship on the server with the normal [installer](../install.md), then run the same commands from a shell on the server:

```sh
ship self setup
ship self status
ship
```

With only one server, the name is optional: `ship setup`, `ship status`.

### sudo

If ship runs as root, it runs the agent and privileged file operations directly, with no sudo; `user` is ignored.

Otherwise ship uses sudo:

- With no `user`, or when `user` is the current user, commands run through `sudo -n`.
- With a different `user`, ship first becomes that user with `sudo -u <user>`, then takes the same sudo path to root.
- Interactive commands such as `logs` and `env edit` run under a TTY, so sudo may prompt for a password (ship omits `-n` there).
- Non-interactive commands use `-n` and fail if sudo needs a password. Configure passwordless sudo for the admin user, or run ship as root from a terminal.

The setup, lock-ssh, proxy, deploy, backup and app commands are unchanged. `setup` installs the same embedded `ship-agent` locally, after checking its version and checksum.

## SSH is still how other machines reach the server

`connection: local` only changes how this machine runs ship. It does not change the server's SSH configuration.

`lock-ssh` still works, and still refuses to lock out the admin user's keys. It only allows the desired `admin_user` and `deploy`, disables root and password logins, and leaves SSH as the only way in from another machine. Do not lock SSH down until the admin user's key works from the machine you will use remotely.

## Image apps

Image apps build on this machine as usual, which is now the server itself. Before the build, ship prints a one-line warning that the build uses this server's CPU and memory.

The image is already in the server's Docker daemon, so ship does not save, gzip or stream it. It verifies the built tag in place before the agent switches to it.

The user running ship needs access to the Docker daemon, either through the `docker` group or by running as root. ship checks this before building and fails with a hint instead of a buildx permission error.

## Other operations

- `env edit` uses the local `sudoedit`.
- `logs` streams from the local Docker daemon.
- `backup --pull` copies archives into `backup_dir_local` on this machine instead of downloading them over SSH.
- `restore` takes a local archive without an SSH upload; ship stages it in `/tmp` for the agent.
- `--dry-run` prints the local commands it would run, just as it prints SSH commands for a remote server.
