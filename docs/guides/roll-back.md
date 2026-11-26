# Roll back

```sh
ship prod rollback <app>
```

In the TUI, choose **Rollback** (`o`) from the app's actions. It asks first.

Rollback switches the app to the previous image recorded in `.ship-state`. It does not restore the database, and it does not run `deploy.commands` from the version being rolled back. Use [restore](back-up-and-restore.md#restore) for data.

Only the current and previous images are kept, so a rollback goes back one version. If the previous image fails its health check, ship prints the logs and leaves the recorded state as it was.

```sh
ship prod status <app>          # current, previous, health
ship prod logs <app>            # follow logs
```
