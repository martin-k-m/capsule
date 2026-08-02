# Commands

```
capsule <command> [flags]
```

`capsule --help` lists the commands; `capsule <command> -h` gives one command's
flags. `capsule version` prints the version.

| Command | What it does |
| :-- | :-- |
| [`init`](#capsule-init) | Write a starter `capsule.toml` |
| [`up`](#capsule-up) | Start an ephemeral capsule and enter it |
| [`shell`](#capsule-shell) | Open another shell in a running capsule |
| [`list`](#capsule-list) | List running capsules |
| [`down`](#capsule-down) | Destroy capsules that outlived their shell |
| [`doctor`](#capsule-doctor) | Check the runtime, config, image and volumes |

Every command except `init`, `list` and `down --all` reads `capsule.toml` from
the current directory.

---

## `capsule init`

Writes `capsule.toml`, detecting the project type.

| Flag | Meaning |
| :-- | :-- |
| `--force` | Overwrite an existing `capsule.toml` |
| `--image IMAGE` | Use this base image instead of the detected one |

Detection checks for a marker file, first match winning: `go.work` and `go.mod` →
Go, `Cargo.toml` → Rust, `pyproject.toml` and `requirements.txt` → Python,
`package.json` → Node. Anything else gets `debian:bookworm-slim`. Each preset
persists that ecosystem's package cache and nothing else.

The generated file is parsed before it is written, so `init` cannot produce
something `up` would then reject.

## `capsule up`

Starts an ephemeral capsule with the project bind-mounted at `workdir`.

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

The exit status is the container's own, so `capsule up -- <cmd>` can be used
anywhere `<cmd>` could.

A TTY is requested only when you gave no command **and** stdin and stdout are
both terminals. That is why the same invocation works unchanged in CI, where
asking for a TTY would fail with an error that has nothing to do with your
project.

## `capsule shell`

Opens another shell in a capsule that is already running — a second window into
the same environment, rather than a second environment.

If several capsules with this project's name are running, it attaches to the
first and says which on stderr. If none are, it says so and suggests `capsule
up` rather than silently starting one.

## `capsule list`

Lists running capsules — name, image, and status — across every project on the
machine, not just this one.

Containers are found by the `me.blinkdev.capsule` label rather than from a state
file, so a capsule left behind by a killed terminal still shows up here.

## `capsule down`

Destroys running capsules.

| Flag | Meaning |
| :-- | :-- |
| `--all` | Destroy every capsule on the machine, not just this project's |

Capsules normally destroy themselves on exit; this exists for when that did not
happen. Without `--all` it needs a `capsule.toml` to know which name to match;
with `--all` it does not, so it works from anywhere.

Persisted volumes are **not** removed — they are the state you asked to keep.
Remove them with your runtime directly (`docker volume rm <name>`).

## `capsule doctor`

Checks, in order: the container runtime and its version, whether the daemon is
reachable, that `capsule.toml` parses, whether the image is present locally, and
which persisted volumes already exist. It also notes any capsule of this project
currently running.

Each line is one of:

| Mark | Meaning |
| :-- | :-- |
| `ok` | Fine |
| `note` | Worth knowing, not a problem — an image not pulled yet, a volume not created yet |
| `FAIL` | A real problem |

Exits non-zero if there was at least one `FAIL`, so it works as a CI
prerequisite check. Notes never affect the exit status.
