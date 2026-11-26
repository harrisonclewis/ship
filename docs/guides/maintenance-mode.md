# Maintenance mode

```sh
ship prod maintenance on <app>
ship prod maintenance off <app>
```

In the TUI, choose **Maintenance on/off** (`m`) from the app's actions; it toggles the current state.

While on, Caddy answers every domain and redirect for the app with a 503 page and `Retry-After: 120`. A flag file in `/opt/proxy/maintenance/` switches it, so Caddy needs no reload. You can also create or remove the flag by hand for manual work on the server.

## During a deploy

Deploys turn the page on before the backup and off after the switch. Set this to skip both steps and keep the site up while the new version starts:

```yaml
deploy:
  maintenance: false
```

If a deploy fails after the deploy commands have run, ship leaves maintenance **on** deliberately; see [When a deploy fails](deploy-an-app.md#when-a-deploy-fails).

## After upgrading ship

Maintenance pages need a mount in the Caddy container that older setups don't have. Run this once per server:

```sh
ship prod setup
ship prod proxy update
```

Until then, deploys of apps with maintenance turned on stop before building or uploading and say so.

The setting is per app; `ship prod status` shows `maintenance=on` when it is active. See [ship.yaml](../reference/ship-yaml.md#keys).
