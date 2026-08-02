# Security

## What capsule is and is not

A capsule is a container. It gives you a **clean, disposable environment** — not
a sandbox for untrusted code. Specifically:

- A capsule shares the host kernel. Container escapes are a real class of bug.
- Your project directory is bind-mounted **writable**. Code running inside a
  capsule can modify your source, and those changes persist on the host.
- Volumes named under `[persist]` survive teardown by design.
- capsule runs whatever `image` your `capsule.toml` names, from whatever registry
  your runtime is configured for. Treat a `capsule.toml` from someone else with
  the same suspicion as any other executable content in a repository.

Do not use a capsule to run code you would not run on your host.

## The published image needs the Docker socket

`ghcr.io/martin-k-m/capsule` can only work if it is given the host's container
socket:

```sh
-v /var/run/docker.sock:/var/run/docker.sock
```

That grants the container **root-equivalent control of the host**. It is
supported because there are legitimate uses for it, but the native binary from
the releases page is the better default.

## Reporting a vulnerability

Report privately through
[GitHub Security Advisories](https://github.com/martin-k-m/capsule/security/advisories/new).
Please do not open a public issue for a vulnerability.

Expect an acknowledgement within 72 hours. Only the latest release is supported.
