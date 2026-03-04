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

## Requirements

- Go 1.25+ (for building/running from source)
- Existing GTNH instance directory
- Network access to GTNH manifests/assets and mod download sources
- Git (for config updates, skipped if missing)
- Optional `GITHUB_TOKEN` for private GitHub downloads or higher API limits

## Installation

Build locally:

```bash
go build -o gtnh-daily-updater .
```

Or install into your Go bin:

```bash
go install .
```

## Quick Start

1. Initialize state for an existing instance:

Note: You MUST pass the current config version your instance has. It will not work correctly otherwise.  
Config versions can be found at https://github.com/GTNewHorizons/GT-New-Horizons-Modpack/releases.

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

Note: Adding CurseForge extra mods requires a CurseForge API key  

Add extra mods from CurseForge (latest release file):

```bash
gtnh-daily-updater extra add SomeMod --source curseforge:12345
```

Add extra mods from CurseForge (pinned file):

```bash
gtnh-daily-updater extra add SomeMod --source curseforge:12345/67890
```

Add extra mods from direct URL:

```bash
gtnh-daily-updater extra add CustomMod --source https://example.com/CustomMod.jar
```

## Profiles

Profiles are stored as TOML files in:

- `${XDG_CONFIG_HOME:-~/.config}/gtnh-daily-updater/profiles`

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

- Default mod cache directory:
  `${XDG_CACHE_HOME:-~/.cache}/gtnh-daily-updater/mods`
- Disable cache with `--no-cache`
- Override cache location with `--cache-dir`
- Control parallel downloads with `--concurrency` (default: `6`)
- Logs are written to `${XDG_CACHE_HOME:-~/.cache}/gtnh-daily-updater/logs/<timestamp>.log`; debug output is always written to the log file regardless of the `-v` flag

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

## Development

Run tests:

```bash
go test ./...
```
