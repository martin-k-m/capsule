# Changelog

All notable changes to capsule are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-08-02

First release.

### Added

- `capsule.toml`: `name`, `image`, `shell`, `workdir`, `ports`, `packages`,
  `[env]` and `[persist]`, read by a hand-rolled TOML-subset parser with no
  third-party dependencies.
- `capsule init` writes a starter config, detecting Go, Rust, Python and Node
  projects and persisting that ecosystem's package cache.
- `capsule up` starts an ephemeral capsule with the project bind-mounted and
  destroys it on exit. `up -- <cmd>` runs one command and exits with its status;
  `--dry-run` prints the runtime command instead of running it.
- `capsule shell` opens another shell in a running capsule.
- `capsule list` / `capsule down [--all]` find and destroy capsules by label.
- `capsule doctor` checks the runtime, daemon, config, image and volumes.
- Container image published to `ghcr.io/martin-k-m/capsule`, plus native
  binaries for Linux, macOS and Windows on the releases page.

[Unreleased]: https://github.com/martin-k-m/capsule/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/martin-k-m/capsule/releases/tag/v0.1.0
