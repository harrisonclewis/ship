# Manage domains

An app's `domains` are all served by Caddy; the first is the canonical domain. `redirects` are optional hostnames that permanently redirect to the canonical domain. Both are part of the app's desired record on the server.

```yaml
domains: [example.com, www.example.com]
redirects: [example.net]
```

- Domains must be lowercase FQDNs with at least one dot and no trailing dot.
- Every domain and redirect must be unique across the desired apps on that server.
- Saving domains or redirects only updates desired config. Apply them with `ship prod proxy restart`; a deploy does not do this for you.

## Point your domain at the server

Every domain and redirect needs an `A` record pointing at the server's IPv4 and, if the server has one, an `AAAA` record pointing at its IPv6. Caddy cannot get a certificate until DNS resolves.

```sh
dig +short A example.com
dig +short AAAA example.com
```

See [Point your domain at the server](../quick-start.md#6-point-your-domain-at-the-server) for the full example.

## Change domains from the command line

These commands update the desired record on the server and validate it against the server's other apps:

```sh
ship prod app add web domains www.example.com
ship prod app remove web domains www.example.com
ship prod app set web domains example.com www.example.com
```

- `set` replaces the whole list.
- `add` appends values that are missing, in order. Values already present are skipped with a note.
- `remove` is atomic: if any value is missing it changes nothing and exits non-zero.
- Removing the last domain is an error. Removing the first (canonical) domain is allowed and prints the new canonical domain.
- Duplicate values in one command are collapsed.
- An empty value clears the list. `domains` must keep at least one entry, so an empty `domains` is rejected.

Redirects use the same commands:

```sh
ship prod app add web redirects www.example.net
ship prod app remove web redirects www.example.net
ship prod app set web redirects ""
```

Saving a route change prints:

```text
Restart Caddy to apply: ship prod proxy restart
```

Run it after the change:

```sh
ship prod proxy restart
```

`proxy restart` renders the Caddyfile from desired domains and redirects, activates the desired `caddy_image` and recreates Caddy, so pending routing changes always apply. `proxy sync` reloads without the forced recreate. Apps that have never been deployed are left out of the rendered Caddyfile.

## Editing the full record

```sh
ship prod app edit web
```

Change `domains` or `redirects` there and save. The same rule applies: desired config changes immediately, routing goes live on `proxy restart`.

## Check what is pending

```sh
ship prod status web
ship prod diff web
```

`status` shows `config=applied`, `config=pending deploy` or `config=not deployed`, and notes when the proxy is out of sync. `diff` prints the desired-vs-running difference and the server-level proxy state. Neither applies the change.

## How redirects behave

Caddy answers each redirect host with a permanent redirect to `https://<canonical>{uri}`. The app's maintenance page applies to redirect hosts too while maintenance is on.

See [ship.yaml](../reference/ship-yaml.md#app-record-keys) for all record keys.
