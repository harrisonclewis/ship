# Add a server

Add the server under `servers:` in `ship.yaml`, then run setup. You need SSH key access as root first; see [Set up SSH access](ssh-access.md). Use `prod` in the examples; replace it with your own server name.

## 1. Add the server

```yaml
servers:
  prod:
    host: root@203.0.113.10   # first setup only
    identity_file: ~/.ssh/prod
    sources:
      web: ~/code/web          # optional, image apps built on this machine
```

- `host` is the ssh destination. Use `root@<ip>` for the very first setup; setup copies root's authorised keys to the admin user.
- `identity_file` is optional. Without it, ssh uses your ssh config and agent.
- `sources` maps an app to its local checkout for image builds. It is optional and never uploaded.

There are no local `admin_user`, `arch`, `caddy_image` or `apps` keys. `admin_user` and `caddy_image` are desired server settings, and `arch` is detected from `uname -m`.

Local keys are in [ship.yaml](../reference/ship-yaml.md#keys). Server names follow the app name rules and can't be a command name or `all`.

With only one server in `ship.yaml`, you can leave the server name out of commands: `ship setup`, `ship deploy hello`.

## 2. Point DNS at the server

Create A/AAAA records for every app domain and redirect before or shortly after setup. Caddy obtains certificates once DNS resolves and retries in the background.

## 3. Prepare the server

Set any non-default server settings first:

```sh
ship prod server set admin_user <name>
ship prod server set caddy_image caddy:2.10-alpine
```

The examples below use the default admin user `ops`. Then:

```sh
ship prod setup
```

Setup reads desired server settings, defaulting to `admin_user: ops` and `caddy_image: caddy:2.10-alpine`, and reads the app uid list from the server's desired records. It installs packages and Docker, creates the admin user with root's SSH keys and passwordless sudo, creates the `deploy` user for CI and app users for desired `uid`s, adds firewall rules, and creates the Docker network, proxy stack directories and nightly backup timer.

## 4. Lock SSH down

Check the admin login first:

```sh
ssh ops@<ip> sudo true
```

Change `host` to `ops@<ip>` in `ship.yaml`, then:

```sh
ship prod lock-ssh
```

`lock-ssh` disables root and password logins and allows only the desired `admin_user` and `deploy`. It refuses to run if the admin user has no authorised keys.

## 5. Activate the proxy

```sh
ship prod proxy sync
```

This renders the Caddyfile from the server's desired records, validates it in a container, activates the desired `caddy_image` and reloads Caddy. It does not force a container restart. Run it after deploying a new app. After changing domains or redirects, use `proxy restart` to apply every pending change:

```sh
ship prod proxy restart
```

Apps that have never been deployed are left out of the Caddyfile. Then [add an app](add-an-app.md).

## What setup does

- Installs packages, including Docker from Docker's repository, and runs a full upgrade.
- Creates the desired `admin_user` with root's SSH keys and passwordless sudo, the `deploy` user, and app users for the desired `uid`s.
- Adds ufw rules for SSH, 80 and 443, keeping existing rules.
- Turns on unattended security updates, including an automatic reboot at 04:00 UTC when one is needed.
- Adds 2 GB swap if the server has none, and caps Docker logs at 10 MB × 3 files.
- Creates the `web` network at `172.30.0.0/24`, `/opt/proxy` (including the `maintenance/` flag folder Caddy reads), `/opt/apps` and the nightly backup timer.

It is safe to re-run and does not overwrite existing swap, ufw rules or users.

After changing `admin_user` or an app's `uid`, setup is pending until you run it:

```sh
ship prod setup
```

`status` and `diff` say when setup is pending.

## Server layout

| Path | What |
|---|---|
| `/usr/local/sbin/ship-agent` | the agent, installed and updated by the CLI ([details](update-ship.md#the-server-agent)) |
| `/opt/proxy/` | Caddy compose file, generated Caddyfile and pinned image |
| `/opt/apps/.ship/server.yaml` | desired `admin_user` and `caddy_image` |
| `/opt/apps/<name>/.ship/desired/` | desired record, compose file and Dockerfile |
| `/opt/apps/<name>/` | running files and state, see below |
| `/opt/apps/ship.yaml` | generated, read-only list of every app's last-deployed config |
| `/var/backups/ship/<name>/` | archives, root 0600 in 0700 directories |
| `web` network | external Docker network `172.30.0.0/24`; only Caddy publishes ports |

Each desired app directory holds:

| File | What |
|---|---|
| `ship-app.yaml` | the authoritative app record |
| `compose.yaml` | the authoritative compose file |
| `Dockerfile` | release apps only |
| `.ship-meta` | metadata generated from the record |
| `.ship-env` | non-secret `env` values generated from the record |
| `.ship-deploy.sh` | `deploy.commands` generated from the record |

A successful deploy promotes those files to `/opt/apps/<name>/` as the running configuration. The running directory also holds `.ship.env`, `.env` (secrets, root 0600, edited with `ship <server> env edit`) and `.ship-state` (current and previous image, plus their typed refs for Git sources).

## Next steps

- [Add an app](add-an-app.md)
- [Deploy an app](deploy-an-app.md)
- [Use ship from another machine](use-from-another-machine.md) — just add the host and key
