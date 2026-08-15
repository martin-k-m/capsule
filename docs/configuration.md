# capsule.toml

One file per project. capsule looks for it in the directory you run it from and
then in each parent, taking the first one it finds, so any command works from
anywhere inside the project. `capsule init` writes a starter version with
everything commented.

The directory holding `capsule.toml` is the project: it is what gets
bind-mounted at `workdir`, and its name is the default capsule `name`. Running
`capsule up` three levels down mounts the whole project, not the level you are
standing on.

```toml
capsule = ">=0.2"

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

## Keys

| Key | Type | Default | Meaning |
| :-- | :-- | :-- | :-- |
| `capsule` | string | none | Oldest capsule that can read this file, `">=MAJOR.MINOR"` |
| `image` | string | **required** | Base image the capsule runs |
| `name` | string | directory name | Capsule name, used for the container name, hostname and labels |
| `shell` | string | `/bin/sh` | Shell to drop into |
| `workdir` | string | `/workspace` | Where the project is mounted inside the container |
| `ports` | array | none | Published as `"host:container"` |
| `packages` | array | none | Installed at start with the image's package manager |
| `[env]` | table | none | Environment variables inside the capsule |
| `[persist]` | table | none | Named volumes that survive teardown |

### `capsule`

The oldest capsule that can read this file, written `">=MAJOR.MINOR"`:

```toml
capsule = ">=0.2"
```

Optional, and `capsule init` writes it. It is checked before anything else in
the file, so a config using a key or a syntax your capsule does not know yet
tells you to upgrade instead of complaining about an unknown key:

```
capsule: capsule.toml: needs capsule >=0.4, but this is capsule 0.2.0; upgrade from https://github.com/martin-k-m/capsule/releases
```

Only `>=` is accepted. A file states the oldest capsule that can read it, which
is a fact about the file. Pinning an exact version would instead lock a config
away from capsules perfectly able to read it.

A prerelease satisfies its own series, so `0.3.0-rc1` meets `">=0.3"`.

Files written before this key existed have no requirement and keep working.
capsule versions before 0.2 do not know the key at all and will reject a file
that carries it, which is exactly the gap the key exists to close from 0.2 on.

### `name`

Must start with a letter or digit and contain only letters, digits, `.`, `-` or
`_`, the same constraint the container runtime puts on a name component. It is
what `capsule shell` and `capsule down` match on, so two projects sharing a name
will find each other's capsules.

### `image`

Required. Passed to the container runtime as an argument, so it may not start
with `-`: it is the only free-form value in this file that lands where a leading
dash would be read as a runtime flag, and a `capsule.toml` from a repository you
just cloned should not be able to choose one.

### `workdir`

Must be absolute. The directory holding `capsule.toml` is bind-mounted here, so
it is the one path inside the capsule whose contents outlive it.

It may not contain a `:`. The runtime splits a mount argument on that character,
so a workdir of `/src:ro` would not name a directory called `src:ro`, it would
mount your project read-only. The same rule applies to a `[persist]` target.

### `ports`

Each entry is `"host:container"`, both numeric and in 1-65535. A bare `"8080"` is
rejected rather than guessed at, because guessing which side you meant would be a
coin flip.

### `packages`

Installed at start using whichever package manager the image actually has: `apk`,
then `apt-get`, then `dnf`. If the image has none, capsule says so on stderr and
carries on rather than failing the capsule or pretending the packages are there.

This is a convenience for a one-off tool, not a build step. It runs on **every**
`capsule up`, so if a capsule needs the same five packages every time, bake them
into an image instead and save yourself the wait.

### `[env]`

Keys become `-e KEY=VALUE`. Keys may not contain `=`, spaces or tabs. Values are
passed through verbatim, so quote anything containing a `#` or it reads as the
start of a comment.

### `[persist]`

Each entry maps a **named volume** to an absolute path inside the container:

```toml
[persist]
gomod = "/go/pkg/mod"
```

These are the only things that survive teardown, and they must be named
explicitly. capsule never infers that something is worth keeping.

The presets `capsule init` writes always persist a *cache*, never source or build
output. That is the line worth holding. Re-downloading a dependency set is pure
waste, so it is worth keeping, while anything a build produced is reproducible
and should go away with the capsule.

Volume names follow the same rules as `name`. The volume is created on first use.

### `[services.<name>]`

Sidecars started alongside the capsule and thrown away with it. One subtable per
service; the subtable name is the hostname the capsule reaches it at.

```toml
capsule = ">=0.2"

name  = "api"
image = "ruby:3.3"

[services.db]
image   = "postgres:16"
ready   = "pg_isready -U postgres"
timeout = "30s"
env     = { POSTGRES_PASSWORD = "dev" }

[services.cache]
image = "redis:7"
ready = "redis-cli ping"
```

`capsule up` starts every service on a private network named after the capsule
and its id, waits for each to be ready, then starts the capsule joined to the
same network. Inside it, `db` and `cache` resolve as hostnames.

| Key | Meaning |
|---|---|
| `image` | required, same as the capsule's own |
| `ready` | shell command run inside the service until it exits 0 |
| `timeout` | how long to wait for `ready`, default 30s |
| `ports` | published to the host as `"host:container"` |
| `env` | passed in as environment variables |

**`ready` is the key worth setting.** A container that is running is not the
same thing as a database accepting connections, and without `ready` the capsule
starts the moment the container does, which for Postgres is several seconds too
early. Omit it and capsule only waits for the container to be up.

Two capsules can run at once: the network name includes the capsule id, so their
services do not collide or reach each other.

Everything is torn down when the capsule exits, including on a signal and on a
service that never becomes ready. A failed start removes what it already
created rather than leaving half a stack running.

Declaring `[services]` needs `capsule = ">=0.2"` in the file. Without it an
older binary reports an unknown key instead of saying the file is newer than it
is.


## Supported syntax

capsule reads a deliberately small subset of TOML: enough for the schema above,
small enough to audit in one sitting, and small enough to need no dependency.

- `#` comments, on their own line or trailing a value
- `[table]` headers, one level deep
- `key = "basic string"` and `key = 'literal string'`
- `key = ["a", "b"]` arrays, which may span several lines
- bare tokens such as `true` or `8080`, kept as their literal text

Escapes `\n`, `\t`, `\\` and `\"` are interpreted inside `"basic strings"` and
left alone inside `'literal strings'`, so a Windows-style path is easiest to
write in single quotes.

Anything outside this subset is a parse error naming the line, rather than a key
that silently does nothing. Unknown keys and unknown `[sections]` are errors too,
so a typo in `wokdir` tells you instead of quietly falling back to the default.

The one thing checked ahead of the syntax is the `capsule` requirement, found in
the raw text if the document does not parse. A file written for a newer capsule
should say so rather than report a line number its author cannot act on.
