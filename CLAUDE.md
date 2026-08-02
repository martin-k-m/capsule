# CLAUDE.md: capsule

Context for AI agents working in this repository.

## What this is

**capsule** creates lightweight, isolated development environments that
disappear when you're done. A `capsule.toml` in a project describes a container;
`capsule up` starts it with the project bind-mounted, drops the developer into a
shell, and destroys it on exit. Repo: <https://github.com/martin-k-m/capsule>.

capsule **drives** a container runtime, it does not replace one. It shells out to
`docker` or `podman` rather than talking to a daemon socket, so it inherits the
developer's existing context and credentials, and every action it takes is one
they could have typed by hand.

Primary distribution is a container image on **GHCR**
(`ghcr.io/martin-k-m/capsule`), with native binaries on the releases page.

## The one promise

Everything in this tool serves a single guarantee: **when a capsule exits,
nothing it created is left behind.**

- Containers are always started with `--rm`. There is no cleanup path that can be
  skipped or fail. `TestRunArgsIsAlwaysEphemeral` exists to keep it that way.
- The *only* exception is `[persist]`, which must stay explicit and opt-in. Do
  not add inference that persists something the user did not name.
- Presets in `capsule init` persist **caches only**, never source or build
  output. Re-downloading a dependency set is waste; a build artifact is
  reproducible and should not survive.
- capsule keeps **no state file**. Containers are found by the
  `me.blinkdev.capsule` label. Do not introduce one. There would be nothing to
  gain and a whole class of drift to lose.

## Build & test

Plain Go, no third-party dependencies:

```sh
go test ./...          # no container runtime required
go vet ./...
gofmt -l .             # must print nothing
go build ./cmd/capsule
```

Keeping `go.mod` dependency-free is deliberate. Before adding a module, check
whether the standard library covers it. The TOML subset reader and the terminal
detection are both there because a dependency was not worth it.

## Layout

```
cmd/capsule/       main, exit-code passthrough
internal/version/  capsule's own version, and the `capsule = ">=x.y"` comparison
internal/config/   capsule.toml: discovery, hand-rolled TOML-subset reader + validation
internal/runtime/  container CLI detection, argv construction, ps filters
internal/cli/      command dispatch: init, up, shell, list, down, doctor
```

`internal/version`, `internal/config` and `internal/runtime` are directly tested;
only `config.Find` and `config.Load` touch the filesystem. `RunArgs` is where the
tool's entire security surface lives, so it stays a pure function that can be
asserted on without Docker present.

`internal/cli` is testable too, and has tests. Its pure parts, the `init`
templates in particular, are the ones worth covering.

## Conventions

- **Honest degradation.** `packages` installs with whatever package manager the
  image has; if it recognises none, it says so on stderr and carries on. Never
  fail the capsule over it, and never pretend the packages are there.
- **Deterministic output.** Map iteration never reaches the user. Env vars,
  persist volumes and error messages all go through sorted key order, and
  `TestRunArgsIsDeterministic` enforces it.
- **Say what survives.** `capsule up` prints what will still exist after exit,
  every time. The promise is the product; state it rather than assume it is
  understood.
- **No TTY guessing.** `-it` is requested only when stdin *and* stdout are both
  terminals, so capsule works unchanged in CI.
- **The project is the config's directory.** `capsule.toml` is found by walking
  upward, and the directory holding it is what gets bind-mounted. Never mount
  `os.Getwd()`; the same project must produce the same capsule from any
  subdirectory.
- **Version before schema.** A config's `capsule` requirement is checked ahead of
  its sections, keys and syntax. An old binary reading a new file must say so,
  not report an unknown key.
- **Nothing free-form reaches argv in flag position.** `image` is the only
  candidate and is validated against a leading `-`. Anything new that lands in
  argv before the image name needs the same treatment.
