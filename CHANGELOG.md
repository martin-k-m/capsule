# Changelog

All notable changes to capsule are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Breaking changes made deliberately before 1.0, while they are still cheap.

### Added

- **`[services.<name>]`: sidecar containers.** A capsule can declare the
  containers it needs, and `capsule up` starts them on a private network named
  after the capsule and its id, waits for each to be ready, then joins the
  capsule to the same network so a service is reachable by its subtable name.
  This is the scenario the README has always opened with and could not do.

  `ready` is a shell command run inside the service until it exits 0. Without
  it the capsule starts as soon as the container does, which for a database is
  several seconds before it accepts connections. `timeout` bounds the wait and
  defaults to 30s.

  Teardown covers every exit: normal, a signal, a service that never becomes
  ready, and a failure partway through starting the stack, which removes what
  it already created rather than leaving half a stack behind. Two capsules can
  run at once without their services colliding, since the network name includes
  the capsule id.

  Requires `capsule = ">=0.2"` in the file, so an older binary says the file is
  newer than it is rather than reporting an unknown key.

### Changed

- **`capsule.toml` is found by walking upward**, and the directory holding it is
  what gets bind-mounted at `workdir`. Previously capsule read and mounted the
  working directory, so running it from a subdirectory either failed or mounted
  a fragment of the project. If you relied on `cd sub && capsule up` mounting
  `sub`, put a `capsule.toml` in `sub`; `capsule init` now says on stderr when
  a file it writes shadows one above it.
- **`image` may not start with `-`.** It is the only free-form value in a
  `capsule.toml` that reaches the container runtime in flag position, so a
  crafted config could previously choose a runtime flag. Configs with such an
  `image` are now rejected at parse time.
- `capsule up` prints the directory it mounts, since that is no longer
  necessarily the directory you ran it from.
- `capsule doctor` reports the project directory it found and the file's
  `capsule` requirement.
- `RunArgs` takes a `RunOptions` struct instead of five positional parameters,
  and the version lives in `internal/version` rather than `internal/cli`. Both
  are internal, so this only affects builds that stamp the version themselves:
  the `-ldflags` path is now
  `github.com/martin-k-m/capsule/internal/version.Current`.

### Added

- **`capsule = ">=MAJOR.MINOR"`**, an optional key naming the oldest capsule that
  can read a file. It is checked before the file's sections, keys and even its
  syntax, so a config written for a newer capsule reports that instead of an
  unknown key or a line number. `capsule init` writes it.

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
