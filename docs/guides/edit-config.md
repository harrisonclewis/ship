# Edit config

`ship.yaml` and each app's `compose.yaml` are plain files. ship can open them in your editor, and can make small app changes for you. Every edit is validated before it is saved, and the previous file is kept as `.bak`.

These commands only touch local files: they never contact a server and `--dry-run` has no effect.

## Edit ship.yaml

```sh
ship config edit
```

This opens the found [`ship.yaml`](../reference/ship-yaml.md#where-ship-looks) in `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows). Values such as `code --wait` are allowed.

ship edits a temporary copy. If you leave it unchanged, nothing is written. Otherwise the result is validated with the same rules as a TUI save, then the original is replaced and the previous file is kept as `.bak`.

An invalid file is not written: ship prints the error and asks `Reopen editor? [Y/n]`, keeping your edits in the temporary copy if you reopen. A save is refused if the real file changed on disk while you were editing.

In the TUI, press `e` on the Servers tab to edit `ship.yaml` the same way.

## Edit a compose file

```sh
ship config edit <app>
```

This opens `apps/<app>/compose.yaml` beside the found `ship.yaml`. If it does not exist yet, the editor starts empty and the file is created on save. The result is checked with the [compose rules](../reference/compose.md) before saving, with the same `.bak`, changed-on-disk and reopen/discard behaviour.

In the TUI, choose **Edit compose.yaml** (`f`) from an app's actions.

## Change one app setting

```sh
ship <server> app set <app> <key> <value...>
ship <server> app add <app> <key> <value...>
ship <server> app remove <app> <key> <value...>
```

The server name is required because these commands find the app through its server; with exactly one server it is optional.

`set` replaces a value. Its keys are:

| Key | Value |
|---|---|
| `source.path` | image app checkout; empty clears it |
| `source.repo` | GitHub `owner/repo` |
| `source.asset` | release asset pattern; empty restores the default |
| `domains` | one or more domains, replacing the list |
| `redirects` | one or more redirects, replacing the list |
| `build.commands` | script; `-` reads it from stdin |
| `deploy.commands` | script; `-` reads it from stdin |
| `deploy.maintenance` | `true` or `false`; empty restores the default `true` |

An unknown key lists the valid ones. For `build.commands` and `deploy.commands`, a value of `-` reads the script from stdin:

```sh
ship prod app set hello deploy.commands - < deploy.sh
```

`add` and `remove` work only on `domains` and `redirects` and accept one or more values. `add` appends missing values in order; values already present are skipped with a note. `remove` is atomic: if any value is missing it changes nothing and exits non-zero. Removing the last domain is an error; removing the first (canonical) domain is allowed and prints the new canonical domain. Duplicate values in one command are collapsed.

These commands write through the same validated save as the TUI. They never contact a server; the next deploy uploads the changed record.

## Edit one app's YAML

```sh
ship <server> app edit <app>
```

This opens only that app's mapping in `$VISUAL`, `$EDITOR` or `vi`, with a header naming the app. The app name and server cannot be changed there; use the TUI's **All settings** to rename.

The edited mapping is spliced back into `ship.yaml` and the whole config is validated before saving, with the same `.bak`, changed-on-disk check and reopen/discard prompt as `ship config edit`.

## Edit from the TUI

On a server page, choose the app and then:

| Key | Screen | What it does |
|---|---|---|
| `i` | app actions | open the **Edit…** submenu |
| `s` | Edit… | **Source**: image path, or release repo and asset |
| `d` | Edit… | **Domains & redirects** |
| `c` | Edit… | **Build & deploy commands** and the maintenance page |
| `y` | Edit… | **Edit app YAML**: only this app's mapping |
| `a` | Edit… | **All settings**: the full app form, including the name and source type |
| `f` | app actions | **Edit compose.yaml** |

The quick forms are prefilled and save through the same validation as the full form. **Edit app YAML** opens the app's mapping, comments included, with a header naming the server and app. The app name is fixed there; rename it with **All settings**. Invalid YAML shows the editor error and offers to reopen or discard, keeping the temporary copy while you decide.

Saving in the TUI:

- checks the whole config first and refuses anything invalid,
- keeps your comments, key order and blank lines,
- keeps the previous file as `.bak`,
- refuses if the file changed on disk since the TUI loaded or opened it,
- creates `~/.config/ship/ship.yaml` if you have no config yet.

Deleting a server or app only removes it from `ship.yaml`; nothing on the server changes. Edits are saved even with `--dry-run`, because they only touch your local files.
