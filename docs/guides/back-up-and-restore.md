# Back up and restore

```sh
ship prod backup [app|all] [--no-pull]    # archive now and download
ship prod backups <app>                   # list server archives
ship prod restore <app> <archive>         # restore one
```

`backup` without an app backs up every app. In the TUI, choose **Backup** (`b`), **Backups** (`L`) or **Restore** (`t`) from an app's actions.

## What an archive holds

Each run writes `/var/backups/ship/<app>/<label>-<UTC timestamp>.tar.gz`, root 0600 in a 0700 directory. It holds `.env` and `.ship-state` when they exist, plus:

| Spec | How |
|---|---|
| `sqlite` | an online `.backup` run as the app uid, then `PRAGMA integrity_check`, then the files |
| `files` | copied with owners and modes |
| `postgres` | `pg_dump` through `docker compose exec`, stored as `pg/<service>.sql` |

SQLite backups run as the app uid because running as root can leave a root-owned `-shm` file the app cannot open. A SQLite backup requires `uid`.

## Schedule and retention

The `ship-backup.timer` runs `ship-agent backup all daily` on the server at 03:30 UTC, with a random delay of up to 10 minutes. It is persistent, so a missed run happens after boot.

| Label | Kept |
|---|---|
| `daily-*` | 14 days |
| `pre-*` | the last 5 |
| `manual-*` | the last 10 |

`pre-*` includes the backup before a deploy (`pre-<version>`) and before a restore (`pre-restore`).

## Downloads

`ship <server> backup` labels archives `manual` and downloads each new one to `<backup_dir_local>/<server>/<app>/` (0700 directories, 0600 files), verifying SHA-256. `--no-pull` keeps them on the server only. If a file with the same name exists with a different checksum, the download stops instead of overwriting it.

Keep an off-server copy, and a separate copy of any encryption key. Without it, database backups cannot be decrypted.

## Restore

```sh
ship prod restore <app> <archive>
```

Restore asks for confirmation, or takes `--yes`. The archive can be a server path under `/var/backups/ship/<app>/`, or a local `.tar.gz`; a local archive is uploaded first. In the TUI, choose a stored archive or **Enter a local path…**.

Before stopping the app, the agent makes sure the archived version's image exists. Then it:

1. Takes a `pre-restore` backup.
2. Stops the app.
3. Restores `.env` and `.ship-state`.
4. Restores files with their original owners and modes, and removes stale SQLite `-wal`/`-shm` files.
5. Starts the recorded image and health-checks it.

Postgres dumps are in the archive but are not replayed automatically. Restore them with `psql` after inspecting the dump.

See [ship.yaml](../reference/ship-yaml.md#keys) for the backup keys.
