# gtnh-daily-updater

CLI tool for keeping an existing GregTech: New Horizons instance up to date with GTNH daily or experimental manifests.

It tracks installed mods, downloads changes, and merges tracked pack files/config updates while preserving user edits when possible.

## Features

- Initializes tracking from an existing instance (`init`)
- Updates mods to manifest-pinned versions (`update`)
- Optional `--latest` mode to use newest non-pre releases when available
- Merges tracked pack files/configs between versions (writes `*.packnew` on conflicts)
- Supports excluded mods and user-defined extra mods
- Supports named profiles and multi-profile batch updates (`update-all`)
- Download caching and configurable concurrency

## Requirements

- Go 1.25+ (for building/running from source)
- Existing GTNH instance directory
- Network access to GTNH manifests/assets and mod download sources
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

```bash
gtnh-daily-updater init \
  --instance-dir "/path/to/instance" \
  --side client
```

Use `--side server` for servers. For experimental packs:

```bash
gtnh-daily-updater init --instance-dir "/path/to/instance" --side client --mode experimental
```

If your instance is older than latest, pass the installed config version:

```bash
gtnh-daily-updater init \
  --instance-dir "/path/to/instance" \
  --side client \
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
- `config diff [--all]`: show tracked file drift from baseline
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
- During pack-file merge conflicts, updater keeps your file and writes pack version as `*.packnew`
- `config diff` compares current tracked files against stored baseline hashes from the last init/update

## Caching and Performance

- Default mod cache directory:
  `${XDG_CACHE_HOME:-~/.cache}/gtnh-daily-updater/mods`
- Disable cache with `--no-cache`
- Override cache location with `--cache-dir`
- Control parallel downloads with `--concurrency` (default: `6`)

## GitHub Token

Provide token via env var:

```bash
export GITHUB_TOKEN=your_token_here
```

Or pass per command:

```bash
gtnh-daily-updater update --github-token your_token_here
```

## Development

Run tests:

```bash
go test ./...
```
