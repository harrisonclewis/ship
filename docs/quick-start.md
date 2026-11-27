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

Create `~/.config/ship/ship.yaml` with your editor:

```yaml
servers:
  prod:
    host: root@<ip>
```

Add `identity_file: ~/.ssh/<key>` if your ssh config and agent don't already offer the key. Once the file exists, `ship config edit` opens it. See [where ship looks](reference/ship-yaml.md#where-ship-looks).

That is the whole local server entry. App records, compose files and Dockerfiles live on the server. If you have an older full-format file with local `apps`, migrate it first:

```sh
ship prod config push
```

## 3. Prepare the server and lock SSH

To use a different admin user, set it before setup:

```sh
ship prod server set admin_user <name>
```

The rest of this guide uses the default `ops`. Then:

```sh
ship prod setup
```

Setup reads the desired server settings, defaulting to admin user `ops` and `caddy:2.10-alpine`, and installs packages and Docker, creates the Caddy stack files, the admin user with root's SSH keys, the firewall rules and the backup timer.

Check the admin login:

```sh
ssh ops@<ip> sudo true
```

Change `prod`'s host to `ops@<ip>` with `ship config edit`. Then:

```sh
ship prod lock-ssh
```

## 4. Add the app

Clone Docker's sample app:

```sh
git clone https://github.com/docker/welcome-to-docker ~/src/welcome-to-docker
```

It is a Node app with a `Dockerfile`; `serve` listens on port 3000.

Create the desired app on the server:

```sh
ship prod app create welcome
```

The editor opens a template. Fill it in:

```yaml
domains: [hello.<yourdomain>]
upstream: welcome:3000
source:
  type: image
backup: {}
```

Then point this machine at the checkout:

```sh
ship prod app set welcome source.path ~/src/welcome-to-docker
```

`source.path` is local only. `ship.yaml` now holds the source:

```yaml
servers:
  prod:
    host: ops@<ip>
    sources:
      welcome: ~/src/welcome-to-docker
```

Change the record later with `ship prod app edit welcome` or `ship prod app set welcome <key> <value>`.

## 5. Add the compose file

```sh
ship prod config edit welcome compose
```

The editor opens the app's desired compose file on the server. Use:

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
ship prod deploy welcome
ship prod proxy sync
ship prod status welcome
```

The first deploy builds the image on your machine from `sources.welcome`, sends it over SSH, starts it and waits for health. The proxy picks up the app's route when you run `proxy sync`. Then open `https://hello.<yourdomain>`.

Later domain or redirect changes need `ship prod proxy restart`; a deploy only applies the app's config, compose and image.

## Next steps

- [Manage domains](guides/manage-domains.md) — add domains, redirects and DNS
- [Edit config](guides/edit-config.md) — change the app from the CLI
- [Back up and restore](guides/back-up-and-restore.md)
- [Use ship from another machine](guides/use-from-another-machine.md)
