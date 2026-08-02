# Architecture

```
cmd/capsule/       main, exit-code passthrough
internal/config/   capsule.toml: TOML-subset reader + validation
internal/runtime/  runtime detection, argv construction, ps filters
internal/cli/      command dispatch: init, up, shell, list, down, doctor
```

Roughly a thousand lines of Go and no third-party dependencies.

## It drives a runtime, it does not replace one

capsule shells out to `docker` or `podman` rather than talking to a daemon socket
itself. Three reasons, in order of how much they mattered:

1. It inherits whatever the developer already configured: context, credentials,
   registry mirrors, rootless setup. None of that has to be reimplemented, and
   none of it can drift out of sync.
2. Every action capsule takes is something the developer could have typed. That
   is what makes `capsule up --dry-run` a genuine explanation rather than an
   approximation of what the tool does internally.
3. It keeps the binary small and the dependency list empty.

`Detect()` prefers `docker`, falls back to `podman`, and reports honestly when
neither is on `PATH`.

## No state file

capsule stamps two labels on every container it starts:

```
me.blinkdev.capsule       = 1
me.blinkdev.capsule.name  = <name>
```

and finds its own containers by querying those labels. It writes no state file,
no lockfile, no `~/.capsule`.

This is a deliberate trade. A state file would let capsule remember slightly
more, but it would also be one more thing that can disagree with reality. Kill a
terminal, reboot mid-session, `docker rm` something by hand, and a state file is
wrong while a label query is still right. `capsule down --all` can sweep a
machine clean even for projects you no longer have checked out, because the truth
lives on the containers.

The container name is `capsule-<name>-<id>`, where `id` is four random bytes, so
two capsules from the same project do not collide.

## Ephemerality is structural, not a cleanup step

Containers are always started with `--rm`. There is no teardown path that can be
skipped, fail, or be missed because a process was killed. The runtime removes the
container and its anonymous volumes when the process ends, whatever ended it.

`TestRunArgsIsAlwaysEphemeral` asserts `--rm` is the second argument, so the
guarantee cannot be quietly removed by a later change.

The only exception is `[persist]`, which stays explicit and opt-in. There is no
inference that decides something is worth keeping.

## The pure core

`internal/config` and `internal/runtime` are pure functions with direct tests.
`RunArgs` in particular is the function that turns a `capsule.toml` into a
runtime argv, which makes it this tool's entire security surface: what gets
mounted, what is exposed, what runs. Keeping it pure means the exact flags a
config produces are asserted in tests that need no container runtime present, on
any platform, in milliseconds.

Two properties are enforced by test rather than convention:

- **Determinism.** Map iteration order never reaches the user. Environment
  variables and persisted volumes go through sorted key order, so the same
  `capsule.toml` always produces byte-identical arguments.
- **Quoting.** Package names and commands reach the container shell as properly
  quoted words. The bootstrap script is generated as a single line, so
  `--dry-run` output stays copy-pasteable.

## Honest degradation

Where capsule depends on something it cannot guarantee, it says so rather than
failing or pretending:

- `packages` installs with whichever of `apk`, `apt-get` or `dnf` the image has.
  If it recognises none, it writes a warning to stderr naming the packages it
  skipped, and the capsule still starts.
- `doctor` distinguishes a problem from a fact. An unpulled image is a `note`,
  not a `FAIL`, and notes never affect the exit status.
- `capsule up` prints what will survive before it starts, every time. The promise
  is the product, so it gets stated rather than assumed.

## Why Go

The container ecosystem is written in Go, a static binary makes the published
image genuinely small, and cross-compiling to five platforms is one `GOOS`
environment variable. The standard library covers everything capsule needs,
including the TOML-subset reader and terminal detection, both of which exist
because a dependency was not worth it.
