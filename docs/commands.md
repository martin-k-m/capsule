# Commands

```
capsule <command> [flags]
```

`capsule --help` lists the commands, `capsule <command> -h` gives one command's
flags, and `capsule version` prints the version.

| Command | What it does |
| :-- | :-- |
| [`init`](#capsule-init) | Write a starter `capsule.toml` |
| [`up`](#capsule-up) | Start an ephemeral capsule and enter it |
| [`shell`](#capsule-shell) | Open another shell in a running capsule |
| [`list`](#capsule-list) | List running capsules |
| [`down`](#capsule-down) | Destroy capsules that outlived their shell |
| [`doctor`](#capsule-doctor) | Check the runtime, config, image and volumes |

Every command except `init`, `list` and `down --all` reads the nearest
`capsule.toml`, searching the current directory and then each parent. The
directory holding that file is the project, and it is what gets mounted, so
these commands work from anywhere inside a project rather than only at its root.

`init` is the exception: it writes into the current directory, wherever that is.

---

## `capsule init`

Writes `capsule.toml`, detecting the project type.

| Flag | Meaning |
| :-- | :-- |
| `--force` | Overwrite an existing `capsule.toml` |
| `--image IMAGE` | Use this base image instead of the detected one |

Detection checks for a marker file, first match winning. `go.work` and `go.mod`
give Go, `Cargo.toml` gives Rust, `pyproject.toml` and `requirements.txt` give
Python, `package.json` gives Node. Anything else gets `debian:bookworm-slim`.
Each preset persists that ecosystem's package cache and nothing else.

The generated file is parsed before it is written, so `init` cannot produce
something `up` would then reject. It carries a `capsule = ">=MAJOR.MINOR"` line
naming the version that wrote it.

`init` writes into the current directory. Since every other command searches
upward, running it in a subdirectory of a project that already has a
`capsule.toml` takes over from the one above, and `init` says so on stderr:

```
capsule: note: this takes over from /home/m/proj/capsule.toml, which is what `capsule up` used here until now
```

## `capsule up`

Starts an ephemeral capsule with the directory holding `capsule.toml`
bind-mounted at `workdir`.

| Flag | Meaning |
| :-- | :-- |
| `--pull` | Pull the image before starting |
| `--dry-run` | Print the runtime command instead of running it |

```sh
capsule up                    # interactive shell
capsule up -- go test ./...   # run one command, then tear down
capsule up --dry-run          # show the exact command, run nothing
```

Everything after `--` is the command to run. With no command, you get the shell
from `capsule.toml`.

Before starting, `up` names what it mounts and what will outlive the capsule:

```
capsule: myapp on golang:1-alpine via docker
capsule: mounting /home/m/proj at /workspace
capsule: on exit only these volumes survive: gomod
```

The mounted directory is the one holding `capsule.toml`, which is not
necessarily the one you ran `capsule` from, which is why it gets named.

The exit status is the container's own, so `capsule up -- <cmd>` can be used
anywhere `<cmd>` could.

A TTY is requested only when you gave no command **and** stdin and stdout are
both terminals. That is why the same invocation works unchanged in CI, where
asking for a TTY would fail with an error that has nothing to do with your
project.

## `capsule shell`

Opens another shell in a capsule that is already running. A second window into
the same environment, rather than a second environment.

If several capsules with this project's name are running, it attaches to the
first and says which on stderr. If none are, it says so and suggests `capsule
up` rather than silently starting one.

## `capsule exec`

Runs one command inside a running capsule and exits with its status.

```sh
capsule exec go test ./...
capsule exec --service db psql -U postgres
capsule exec -- ls -la          # -- when the command starts with a dash
```

`shell` is for a person and this is for a script. A CI job, a git hook, a
Makefile target or an editor's "run tests" button all want to run one command in
the project's environment and read the result, and the only way to do that
before was to open an interactive shell and type into it.

**The exit code is the point.** `capsule exec go test ./...` fails when the
tests fail. Without that, every use of it in CI passes silently.

A TTY is requested only when stdin and stdout are both terminals, and never with
`--no-tty`. A TTY in a pipeline is worse than useless: the runtime turns on line
editing and colour, and whatever is reading the output gets escape sequences
mixed into what it is parsing. `-i` stays on regardless, so
`echo hi | capsule exec cat` works.

`--service NAME` runs in a sidecar instead of the capsule. A name that is not in
`capsule.toml` is an error that lists the ones that are, rather than a container
lookup that finds nothing and says so in the runtime's words.

## `capsule list`

Lists running capsules by name, image, and status, across every project on the
machine rather than just this one.

Containers are found by the `me.blinkdev.capsule` label rather than from a state
file, so a capsule left behind by a killed terminal still shows up here.

## `capsule down`

Destroys running capsules.

| Flag | Meaning |
| :-- | :-- |
| `--all` | Destroy every capsule on the machine, not just this project's |

Capsules normally destroy themselves on exit. This exists for when that did not
happen. Without `--all` it needs a `capsule.toml` to know which name to match.
With `--all` it does not, so it works from anywhere.

Persisted volumes are **not** removed. They are the state you asked to keep.
Remove them with your runtime directly (`docker volume rm <name>`).

## `capsule doctor`

Checks, in order: the container runtime and its version, whether the daemon is
reachable, that `capsule.toml` parses, which directory it will mount and where,
the file's `capsule` requirement if it declares one, whether the image is present
locally, and which persisted volumes already exist. It also notes any capsule of
this project currently running.

Each line is one of:

| Mark | Meaning |
| :-- | :-- |
| `ok` | Fine |
| `note` | Worth knowing but not a problem, such as an image not pulled yet |
| `FAIL` | A real problem |

Exits non-zero if there was at least one `FAIL`, so it works as a CI
prerequisite check. Notes never affect the exit status.
