# Install

## One-line install

Linux and macOS (x64 or arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/harrisonclewis/ship/main/install.sh | bash
```

Windows (PowerShell, x64 or arm64):

```powershell
irm https://raw.githubusercontent.com/harrisonclewis/ship/main/install.ps1 | iex
```

The scripts download the latest release from GitHub and check it against the release's `SHA256SUMS` before installing.

| Setting | Linux and macOS | Windows |
|---|---|---|
| Install folder | `~/.local/bin` | `%LOCALAPPDATA%\Programs\ship` |
| Change the folder | `SHIP_INSTALL_DIR=/path` | `$env:SHIP_INSTALL_DIR = 'C:\path'` |
| A specific release | `SHIP_VERSION=0.1.0` | `$env:SHIP_VERSION = '0.1.0'` |

On Linux and macOS the script warns if the install folder isn't on your `PATH`. On Windows it adds the folder to your user `PATH`; open a new terminal afterwards.

## Manual download

Download the file for your system from the [releases page](https://github.com/harrisonclewis/ship/releases):

| Platform | File |
|---|---|
| Linux x64 | `ship-linux-x64` |
| Linux arm64 | `ship-linux-arm64` |
| macOS (Apple Silicon) | `ship-darwin-arm64` |
| macOS (Intel) | `ship-darwin-x64` |
| Windows x64 | `ship-windows-x64.exe` |
| Windows arm64 | `ship-windows-arm64.exe` |

- **macOS:** the binaries are ad-hoc signed but not notarised. If you downloaded one with a browser, run `xattr -d com.apple.quarantine <file>` before launching it.
- **Windows:** a browser-downloaded binary may trigger SmartScreen; choose **More info** → **Run anyway** if you trust the release.

## Build from source

You need Go 1.27 or newer.

```sh
make build        # ./bin/ship
```

The binary embeds `ship-agent`, so there is nothing else to copy to the server. A source build reports version `dev` and never updates itself.

## Requirements

| Tool | Why |
|---|---|
| OpenSSH client | every server action runs over `ssh`. On Windows, install the [OpenSSH Client](https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_install_firstuse) and use Windows Terminal |
| Docker with buildx | only for [image apps](deploying.md#image-apps), which are built on your machine |
| git | only for image apps, to name the version |

Then [set up your config](configuration.md).
