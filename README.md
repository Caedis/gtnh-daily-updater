# gtnh-daily-updater

CLI tool for keeping an existing GregTech: New Horizons instance up to date with GTNH daily or experimental manifests.

It tracks installed mods, downloads changes, and applies config updates using git to merge pack changes while preserving user edits.

## Warning
This is only used for updating from a daily to a daily or an experimental to an experimental  
If coming from the old gtnh-nightly-updater jar, it is best to start from scratch and manually copy any user added mods and config changes (as well as the other folders on the wiki page about updating)

Always make a full instance backup before first using this program

## Features

- Initializes tracking from an existing instance (`init`)
- Updates mods to manifest-pinned versions (`update`)
- Optional `--latest` mode to use newest non-pre releases when available (Only use if you know what you are doing)
- Tracks config files with a local git branch; merges pack updates automatically (pack wins on conflicts)
- Supports excluded mods and user-defined extra mods
- Supports named profiles and multi-profile batch updates (`update-all`)
- Download caching and configurable concurrency
- Optional startup self-update check with SHA256-verified `self-update` command

## Requirements

- Go 1.25+ (for building/running from source)
- Existing GTNH instance directory
- Network access to GTNH manifests/assets and mod download sources
- Git (for config updates, skipped if missing)
- Optional `GITHUB_TOKEN` for private GitHub downloads or higher API limits

## Installation

[Releases are available for Linux, Windows, and MacOS](https://github.com/Caedis/gtnh-daily-updater/releases)  
No Go required  
Just download the version for your OS and run it from the command line.  
Note: I suggest putting it somewhere in your PATH so it can be ran from anywhere.  
On Linux, I have `~/.local/bin` added to my PATH  

Building from source code:

```bash
go build -o gtnh-daily-updater .
```

Or install into your Go bin:

```bash
go install .
```

**Building on Windows** (to avoid UAC admin prompts):

On Windows, the build process generates a manifest file that prevents unnecessary UAC prompts. To build with the manifest:

```powershell
# Install rsrc (one-time setup)
go install github.com/akavel/rsrc@v0.10.2

# Generate the Windows manifest resource
rsrc -manifest gtnh-daily-updater.manifest -arch amd64 -o rsrc_windows_amd64.syso

# Build the executable (go will automatically link the .syso file)
go build -o gtnh-daily-updater.exe .
```

For another Windows architecture (for example, 386), keep `GOARCH` and `rsrc -arch` aligned:

```powershell
$env:GOARCH = "386"
rsrc -manifest gtnh-daily-updater.manifest -arch $env:GOARCH -o "rsrc_windows_$($env:GOARCH).syso"
go build -o gtnh-daily-updater.exe .
```

## Quick Start

1. Initialize state for an existing instance:

Note: **You MUST pass the current config version your instance has. It will not work correctly otherwise.**  
Config versions can be found at https://github.com/GTNewHorizons/GT-New-Horizons-Modpack/releases.
For Daily and Experimental builds, the config version is found in the `gtnh-XXXXXX-manifest.json` build artifact and will likely be `2.X.0-nightly-<daily build date>`.

For MultiMC/Prism clients, the instance-dir should be the folder that contains the `.minecraft` folder, not the `.minecraft` folder itself.  
For servers, the instance-dir is the root of the server folder, the one that contains `mods`, `config`, etc.

```bash
gtnh-daily-updater init \
  --instance-dir "/path/to/instance" \
  --side client \
  --config 2.9.0-nightly-2026-02-10
```

Use `--side server` for servers.

For experimental packs:

```bash
gtnh-daily-updater init \
  --instance-dir "/path/to/instance" \
  --side client \
  --mode experimental \
  --config 2.9.0-nightly-2026-02-10
```

2. Check current status:

```bash
gtnh-daily-updater status --instance-dir "/path/to/instance"
```

3. Preview changes:

```bash
gtnh-daily-updater update --instance-dir "/path/to/instance" --dry-run
```

4. Apply update:

```bash
gtnh-daily-updater update --instance-dir "/path/to/instance"
```

## Common Commands

- `update`: apply a single-instance update
- `update-all <profile> [profile...]`: update multiple saved profiles sequentially
- `status`: compare local state vs latest manifest
- `config diff [--all] [path]`: show tracked file drift, or file-level diff for one path
- `exclude add|remove|list`: skip selected manifest mods
- `extra add|remove|list`: manage non-manifest mods
- `profile create|list|show|delete`: manage reusable option sets
- `self-update`: download and install the latest release after SHA256 verification

Inspect all options:

```bash
gtnh-daily-updater --help
gtnh-daily-updater <command> --help
```

## Excludes and Extra Mods

Exclude manifest mods:

```bash
gtnh-daily-updater exclude add ModName AnotherMod
gtnh-daily-updater exclude list
```

Add extra mods from assets DB (default source):

```bash
gtnh-daily-updater extra add Angelica
```

Add extra mods from GitHub releases:

```bash
gtnh-daily-updater extra add SomeMod --source github:Owner/Repo
```

Add extra mods from CurseForge (latest release file, requires CurseForge API key):

```bash
gtnh-daily-updater extra add SomeMod --source curseforge:12345
```

Add extra mods from CurseForge (pinned file):

```bash
gtnh-daily-updater extra add SomeMod --source curseforge:12345/67890
```

Add extra mods from Modrinth (latest release, no API key needed, rate limit 300/min/ip):

```bash
gtnh-daily-updater extra add SomeMod --source modrinth:slug-or-id
```

Add extra mods from Modrinth (pinned version):

```bash
gtnh-daily-updater extra add SomeMod --source modrinth:slug-or-id/versionID
```

CurseForge and Modrinth "latest" sources default to the release channel. Append
`@beta` or `@alpha` to opt into less stable channels; a channel is cumulative,
so `@beta` still picks a newer release over an older beta:

```bash
gtnh-daily-updater extra add jei --source curseforge:238222@beta
gtnh-daily-updater extra add journeymap --source modrinth:journeymap@alpha
```

Channels do not apply to pinned sources (`curseforge:12345/67890`,
`modrinth:slug/versionID`).

Add extra mods from direct URL:

```bash
gtnh-daily-updater extra add CustomMod --source https://example.com/CustomMod.jar
```

Pick a specific jar from a GitHub release that ships multiple variants. Use `--match <regex>` to select the asset; the pattern must uniquely match one `.jar`, otherwise the candidate names are listed so you can refine. Anchor patterns can be used (e.g. `"unlimited\.jar$"` for [JourneyMap](https://github.com/TeamJM/journeymap-legacy), `"-\d+\.\d+\.\d+\.jar$"` for [GTNH-Web-Map](https://github.com/GTNewHorizons/GTNH-Web-Map)) to avoid matching `-dev`, `-sources`, or `-preshadow` variants:

```bash
gtnh-daily-updater extra add journeymap \
    --source github:TeamJM/journeymap-legacy \
    --match "unlimited\.jar$"
```

A same-name extra overrides the manifest entry — no need to `exclude` the original version first. This is the supported way to swap, for example, the manifest's `journeymap-fairplay` for the unlimited build from the same release.

## Profiles

Profiles are stored as TOML files under the OS-native user config directory:

- Linux: `${XDG_CONFIG_HOME:-~/.config}/gtnh-daily-updater/profiles`
- macOS: `~/Library/Application Support/gtnh-daily-updater/profiles`
- Windows: `%AppData%\gtnh-daily-updater\profiles`

Profiles previously stored under `~/.config/gtnh-daily-updater` on macOS/Windows are auto-migrated to the new location on first run.

Create and use a profile:

```bash
gtnh-daily-updater profile create main-client \
  --instance-dir "/path/to/instance" \
  --side client \
  --concurrency 8

gtnh-daily-updater update --profile main-client
```

Batch update profiles:

```bash
gtnh-daily-updater update-all main-client alt-server
```

## State, Paths, and Merge Behavior

- Local state is stored at `<instance-dir>/.gtnh-daily-updater.json`
- On Prism/MultiMC layouts, game files are resolved under `<instance-dir>/.minecraft/`
- On server/other layouts, game files are resolved directly under `<instance-dir>/`
- Config files are tracked in a git repo at `<game-dir>/.gtnh-configs/` on a `local` branch; pack updates are applied via `git merge -X theirs` (pack wins on conflicts)
- Tracked items: `config/`, `journeymap/` (preserving `data/`), `resourcepacks/` (client only), `serverutilities/`, `servers.json` (client only)
- `config diff` shows your changes relative to the pack version (`git diff <configVersion>..local`)
- `config diff "GregTech/Pollution.cfg"` shows diff for a specific file (also accepts `config/GregTech/Pollution.cfg`)
- Config tracking requires git; config updates are skipped gracefully if git is unavailable or the repo hasn't been initialized yet

## Caching and Performance

- Default mod cache and log directory lives under the OS-native user cache directory:
  - Linux: `${XDG_CACHE_HOME:-~/.cache}/gtnh-daily-updater/{mods,logs}`
  - macOS: `~/Library/Caches/gtnh-daily-updater/{mods,logs}`
  - Windows: `%LocalAppData%\gtnh-daily-updater\{mods,logs}`
- Caches/logs previously stored under `~/.cache/gtnh-daily-updater` on macOS/Windows are auto-migrated to the new location on first run.
- Disable cache with `--no-cache`
- Override cache location with `--cache-dir`
- Control parallel downloads with `--concurrency` (default: `6`)
- Logs are written to `<cache-dir>/logs/<timestamp>.log`; debug output is always written to the log file regardless of the `-v` flag

## GitHub Token

Provide token via env var:

```bash
export GITHUB_TOKEN=your_token_here
```

Or pass per command:

```bash
gtnh-daily-updater update --github-token your_token_here
```

## CurseForge API Key

Required to use `curseforge:` extra mod sources. Get a key at https://console.curseforge.com/.

Provide via env var:

```bash
export CURSEFORGE_API_KEY=your_key_here
```

Or pass per command:

```bash
gtnh-daily-updater update --curseforge-key your_key_here
```

## Self-Update

The tool can check GitHub for newer releases at startup and install them on demand. Both behaviors are off by default.

A global config file is created on first run at:

- Linux: `${XDG_CONFIG_HOME:-~/.config}/gtnh-daily-updater/config.toml`
- macOS: `~/Library/Application Support/gtnh-daily-updater/config.toml`
- Windows: `%AppData%\gtnh-daily-updater\config.toml`

```toml
auto_update_check = false
include_prereleases = false
```

When `auto_update_check = false` (default), the tool prints a one-line hint pointing to the config file. When `true`, each invocation queries GitHub (3s timeout, silent on failure) and prints a notice if a newer release is available.

Install the latest release:

```bash
gtnh-daily-updater self-update            # prompts for confirmation
gtnh-daily-updater self-update --yes      # skip prompt (non-interactive)
gtnh-daily-updater self-update --prerelease
```

The downloaded zip's SHA256 is verified against the digest published by the GitHub release API before the running binary is replaced. On Windows, the previous binary is left as `<exe>.old` and removed on next startup.

## Development

Run tests:

```bash
go test ./...
```
