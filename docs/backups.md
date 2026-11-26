# Backups and restore

```sh
ship prod backup [app|all] [--no-pull]    # archive now and download
ship prod backups <app>                   # list server archives
ship prod restore <app> <archive>         # restore one
```

## What an archive holds

Each run writes `/var/backups/ship/<app>/<label>-<UTC timestamp>.tar.gz`, root 0600 in a 0700 directory. It holds `.env` and `.ship-state` when they exist, plus:

| Spec | How |
|---|---|
| `sqlite` | an online `.backup` run as the app uid, then `PRAGMA integrity_check`, then the files |
| `files` | copied with owners and modes |
| `postgres` | `pg_dump` through `docker compose exec` |

SQLite backups run as the app uid because running as root can leave a root-owned `-shm` file the app cannot open.

## Schedule and retention

The `ship-backup.timer` runs `ship-agent backup all daily` on the server at 03:30 UTC.

| Label | Kept |
|---|---|
| `daily-*` | 14 days |
| `pre-*` | the last 5 |
| `manual-*` | the last 10 |

## Downloads

`ship <server> backup` labels archives `manual` and downloads each new one to `<backup_dir_local>/<server>/<app>/` (0700 directories, 0600 files), verifying SHA-256. `--no-pull` keeps them on the server only.

Keep an off-server copy, and a separate copy of any encryption key. Without it, database backups cannot be decrypted.

## Restore

Restore asks for confirmation, or `--yes`. A local archive path is uploaded first. The agent then:

1. Takes a `pre-restore` backup.
2. Stops the app.
3. Restores files with their original owners and modes and removes stale SQLite `-wal`/`-shm` files.
4. Restores `.ship-state` and `.env`.
5. Starts the recorded image and health-checks it.

Postgres dumps are in the archive but are not replayed automatically. Restore them with `psql` after inspecting the dump.
