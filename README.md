# capsule

**Lightweight, isolated development environments that disappear when you're done.**

[![CI](https://github.com/martin-k-m/capsule/actions/workflows/ci.yml/badge.svg)](https://github.com/martin-k-m/capsule/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-00ADD8?style=flat-square&logo=go&logoColor=fff)](https://go.dev)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

You need Postgres 14 and an old Node to reproduce a bug. You try a build with a
different toolchain. You run someone else's repo. Every one of those leaves
something behind — a global package, a version manager entry, a daemon on 5432,
a `node_modules` you did not want.

A capsule is a container described by one file in your project. `capsule up`
starts it with your project mounted, drops you into a shell, and destroys it on
exit. Your source is on the host and outlives the capsule. Everything else is
gone, unless you named it under `[persist]`.

```sh
capsule init      # writes capsule.toml, guessing from your project
capsule up        # you are now inside; type exit and it never existed
```

## Install

Prebuilt binaries for Linux, macOS and Windows are on the
[releases page](https://github.com/martin-k-m/capsule/releases).

Or run the published image — see the [security note](#running-capsule-in-a-container)
before you do:

```sh
docker pull ghcr.io/martin-k-m/capsule:latest
```

Or build it yourself (no third-party dependencies, so this is just Go):

```sh
go install github.com/martin-k-m/capsule/cmd/capsule@latest
```

capsule needs `docker` or `podman` on your PATH. It does not replace them — it
drives the one you already have.

## capsule.toml

```toml
name    = "myapp"
image   = "golang:1-alpine"
shell   = "/bin/sh"
workdir = "/workspace"

ports    = ["8080:8080"]
packages = ["git", "make"]

[env]
CGO_ENABLED = "0"

# The only state a capsule keeps.
[persist]
gomod = "/go/pkg/mod"
```

| Key | Meaning |
| :-- | :-- |
| `name` | Capsule name; defaults to the directory name |
| `image` | **Required.** Base image the capsule runs |
| `shell` | Shell to drop into (default `/bin/sh`) |
| `workdir` | Where the project is mounted (default `/workspace`) |
| `ports` | Published as `"host:container"` |
| `packages` | Installed at start with the image's `apk`/`apt-get`/`dnf` |
| `[env]` | Environment variables inside the capsule |
| `[persist]` | Named volumes that survive teardown |

`packages` is a convenience for a one-off tool, not a build step — if a capsule
needs the same five packages every time, bake them into an image instead.

## Commands

| Command | What it does |
| :-- | :-- |
| `capsule init` | Write a starter `capsule.toml`, detecting Go, Rust, Node or Python |
| `capsule up` | Start an ephemeral capsule and enter it |
| `capsule up -- <cmd>` | Run one command in a fresh capsule and tear it down |
| `capsule up --dry-run` | Print the exact runtime command instead of running it |
| `capsule shell` | Open another shell in a capsule that is already running |
| `capsule list` | List running capsules |
| `capsule down [--all]` | Destroy capsules that outlived their shell |
| `capsule doctor` | Check the runtime, the config, the image and the volumes |

`capsule up -- <cmd>` exits with the command's own status, so it drops into a
CI step or a shell pipeline without wrapping:

```sh
capsule up -- go test ./...
```

## What "disappears" actually means

capsule makes one promise, so it is worth being exact about it.

**Goes away on exit:** the container, its filesystem, anything installed into it,
anonymous volumes, and processes it started. The container is run with `--rm`;
there is no cleanup step that can be skipped or fail.

**Survives:** your project directory, because it is a bind mount from the host
and never a copy — and the named volumes under `[persist]`, which exist so that
a package cache does not have to be re-downloaded every session.

**Not isolation from the host.** A capsule is a container, with a container's
boundaries: it shares your kernel, and your project directory is writable from
inside. It is a clean, disposable environment — not a sandbox for untrusted code.

capsule keeps no state file. It finds its own containers by label, so nothing
can drift out of sync, and `capsule down --all` can always sweep a machine clean.

## Running capsule in a container

The published image needs the host's Docker socket to do anything, because
capsule drives a container runtime:

```sh
docker run --rm -it \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:$PWD" -w "$PWD" \
  ghcr.io/martin-k-m/capsule up
```

Mounting that socket gives the container root-equivalent control of the host.
That is a real trade, not a formality — for everyday use, prefer the native
binary.

## Documentation

| Document | What it covers |
| :-- | :-- |
| [docs/getting-started.md](docs/getting-started.md) | Install, first capsule, running one command |
| [docs/configuration.md](docs/configuration.md) | Every `capsule.toml` key |
| [docs/commands.md](docs/commands.md) | Every command and flag |
| [docs/architecture.md](docs/architecture.md) | How it works, and why it is built this way |

## Development

```sh
go test ./...          # unit tests, no container runtime required
go vet ./...
go build ./cmd/capsule
```

The parts that decide what a `capsule.toml` becomes — the config reader and the
runtime argv builder — are pure functions with direct tests, so the flags a
capsule turns into can be verified without Docker anywhere in sight.

## License

[Apache-2.0](LICENSE)
