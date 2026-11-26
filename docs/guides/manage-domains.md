# Manage domains

An app's `domains` are all served by Caddy; the first is the canonical domain. `redirects` are optional hostnames that permanently redirect to the canonical domain.

```yaml
apps:
  web:
    domains: [example.com, www.example.com]
    redirects: [example.net]
```

- Domains must be lowercase FQDNs with at least one dot and no trailing dot.
- Every domain and redirect must be unique across every app on every server.
- After changing domains or redirects, run `ship prod proxy update` to render and reload the Caddyfile. A deploy does not do this for you.

## Point your domain at the server

Every domain and redirect needs an `A` record pointing at the server's IPv4 and, if the server has one, an `AAAA` record pointing at its IPv6. Caddy cannot get a certificate until DNS resolves.

```sh
dig +short A example.com
dig +short AAAA example.com
```

See [Point your domain at the server](../quick-start.md#6-point-your-domain-at-the-server) for the full example.

## Change domains from the command line

These commands edit only the local `ship.yaml` and validate the whole file before saving:

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
- An empty value clears the list; `domains` still needs at least one entry to validate.

Redirects use the same commands:

```sh
ship prod app add web redirects www.example.net
ship prod app remove web redirects www.example.net
ship prod app set web redirects ""
```

Then update the proxy:

```sh
ship prod proxy update
```

## Change domains in the TUI

On the server page, choose the app, **Edit…** (`i`), then **Domains & redirects** (`d`). The full app form under **All settings** (`a`) also edits them. Saving writes `ship.yaml`; run **Proxy update** (`p`) on the server page afterwards.

## How redirects behave

Caddy answers each redirect host with a permanent redirect to `https://<canonical>{uri}`. The app's maintenance page applies to redirect hosts too while maintenance is on.

See [ship.yaml](../reference/ship-yaml.md#keys) for all app keys.
