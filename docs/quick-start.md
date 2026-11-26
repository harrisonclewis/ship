# Quick start

This takes you from nothing to a live app at `https://hello.<yourdomain>`.

You need:

- A fresh Ubuntu VPS. Setup uses `apt` and installs Docker from Docker's Ubuntu repository.
- Root SSH key access to the server ([set it up](guides/ssh-access.md)), its IPv4 address, and its IPv6 address if it has one.
- A domain you control.
- OpenSSH, Git and Docker with buildx on the machine where you run ship.

## 1. Install ship

```sh
curl -fsSL https://raw.githubusercontent.com/harrisonclewis/ship/main/install.sh | bash
```

Windows and manual installs are in [Install](install.md).

## 2. Add the server

In the TUI:

```sh
ship
```

Press `a` on the Servers tab. Name it `prod`, set the host to `root@<ip>`, and press `Enter`. This creates `~/.config/ship/ship.yaml`.

Or create `~/.config/ship/ship.yaml` with your editor:

```yaml
servers:
  prod:
    host: root@<ip>
    apps: {}
```

Add `identity_file: ~/.ssh/<key>` if your ssh config and agent don't already offer the key. Once the file exists, `ship config edit` opens it. See [where ship looks](reference/ship-yaml.md#where-ship-looks).

## 3. Prepare the server and lock SSH

```sh
ship prod setup
```

Setup installs packages and Docker, creates the Caddy stack files, the `ops` admin user with root's SSH keys, the firewall rules and the backup timer.

Check the admin login:

```sh
ssh ops@<ip> sudo true
```

Change `prod`'s host to `ops@<ip>`: open the server in the TUI and press `e`, or run `ship config edit`. Then:

```sh
ship prod lock-ssh
```

## 4. Add the app

Clone Docker's sample app:

```sh
git clone https://github.com/docker/welcome-to-docker ~/src/welcome-to-docker
```

It is a Node app with a `Dockerfile`; `serve` listens on port 3000.

In the TUI, open `prod`, press `a`, and fill in:

| Field | Value |
|---|---|
| Name | `welcome` |
| Domains | `hello.<yourdomain>` |
| Upstream | `welcome:3000` |
| Source type | `Local image` |
| Local image source path | `~/src/welcome-to-docker` |

In `ship.yaml`, the same app is:

```yaml
servers:
  prod:
    apps:
      welcome:
        domains: [hello.<yourdomain>]
        upstream: welcome:3000
        source:
          type: image
          path: ~/src/welcome-to-docker
```

To change the path later, run `ship prod app set welcome source.path <path>`.

## 5. Add the compose file

```sh
ship config edit welcome
```

This opens an empty `apps/welcome/compose.yaml`. Use:

```yaml
name: welcome

services:
  welcome:
    image: ${IMAGE:?run ship deploy welcome first}
    restart: unless-stopped
    networks: [web]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 256m
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:3000/"]
      interval: 10s
      timeout: 3s
      start_period: 10s
      retries: 3

networks:
  web:
    external: true
```

## 6. Point your domain at the server

For every domain and redirect, add:

| Type | Name | Value |
|---|---|---|
| `A` | `hello.<yourdomain>` | the server's IPv4, for example `203.0.113.10` |
| `AAAA` | `hello.<yourdomain>` | the server's IPv6, for example `2001:db8::10` |

Only add `AAAA` if the server has IPv6. Find it in your provider's panel or on the server:

```sh
ssh ops@<ip> 'ip -6 addr show scope global'
```

Check the records:

```sh
dig +short A hello.<yourdomain>
dig +short AAAA hello.<yourdomain>
```

DNS changes can take time to propagate. Caddy cannot get a certificate until DNS points at the server; it retries in the background.

## 7. Deploy

```sh
ship prod proxy update
ship prod deploy welcome
ship prod status welcome
```

The first deploy builds the image on your machine, sends it over SSH, starts it and waits for health. Then open `https://hello.<yourdomain>`.

## Next steps

- [Manage domains](guides/manage-domains.md) — add domains, redirects and DNS
- [Edit config](guides/edit-config.md) — change the app from the CLI or TUI
- [Back up and restore](guides/back-up-and-restore.md)
- [Use ship from another machine](guides/use-from-another-machine.md)
