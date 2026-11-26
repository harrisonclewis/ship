# Server records

Every server keeps a record of the apps deployed on it. When you add a server that already runs apps, from another machine or after losing your local config, ship shows those apps and can import them into your `ship.yaml`.

Your local `ship.yaml` stays the source of truth: deploys always use it, and builds still happen on your machine. The server's copy is the **last-deployed record** of each app.

## What's stored

Each deploy uploads the app's config as `/opt/apps/<app>/ship-app.yaml`, next to its `compose.yaml` and `Dockerfile`. ship also keeps a combined, read-only `/opt/apps/ship.yaml` you can read on the server.

The record holds everything in the app's `ship.yaml` block except `source.path`, which only makes sense on one machine. Secrets in `/opt/apps/<app>/.env` are never part of it.

Apps deployed by ship v0.1.0 have no record until their next deploy; they show as **no record (older ship)**.

## In the TUI

On a server page:

- **on server**: an app the server runs that your `ship.yaml` doesn't have. Choose **Import app** to copy its config into `ship.yaml` and its `compose.yaml`/`Dockerfile` into `apps/<name>/`.
- **DIFF**: your local config or files differ from what was last deployed, for example because someone deployed from another machine. **Compare with server** shows the differences. **Use server version** copies the server's config and files over yours, or **Keep mine** leaves things as they are, and your version goes up on the next deploy.

After you add a server, ship reads its records and offers to import every app it finds.

Importing keeps your comments, your local `source.path`, and a `.bak` of any file it overwrites. Files that are already identical are left alone.

## From the command line

```sh
ship prod diff [app]                      # what differs, and which apps are only local or only on the server
ship prod import blog                     # import one app
ship prod import --all                    # import every server app you don't have
ship prod import blog --overwrite         # replace your local copy; keeps .bak files
```

## Image apps without a source path

An imported image app has no `source.path`. You can still redeploy versions already on the server, roll back, back up and restore. To build a new version, set `source.path` to your local checkout, in `ship.yaml` or with **Edit app**.
