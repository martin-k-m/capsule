# capsule documentation

capsule turns a `capsule.toml` into a container with your project mounted, drops
you into a shell, and destroys it on exit.

| Document | What it covers |
| :-- | :-- |
| [getting-started.md](getting-started.md) | Install, first capsule, running one command |
| [configuration.md](configuration.md) | Every `capsule.toml` key |
| [commands.md](commands.md) | Every command and flag |
| [architecture.md](architecture.md) | How it works, and why it is built this way |

## The one promise

Everything here serves a single guarantee. When a capsule exits, nothing it
created is left behind.

What goes away: the container, its filesystem, anything installed into it,
anonymous volumes, and processes it started.

What survives: your project directory, because it is a bind mount from the host
and never a copy, plus the named volumes you listed under `[persist]`.

What this is *not* is isolation from the host. A capsule is a container, with a
container's boundaries. It shares your kernel, and your project directory is
writable from inside. Treat it as a clean, disposable environment rather than a
sandbox for untrusted code. See [SECURITY.md](../SECURITY.md).
