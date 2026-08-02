# Getting started

## Install

capsule needs `docker` or `podman` on your `PATH`. It does not replace them. It
drives whichever one you already have.

Prebuilt binaries for Linux, macOS and Windows are on the
[releases page](https://github.com/martin-k-m/capsule/releases). You can also
build it yourself. capsule has no third-party dependencies, so this is just Go:

```sh
go install github.com/martin-k-m/capsule/cmd/capsule@latest
```

There is also a published image, `ghcr.io/martin-k-m/capsule`, but read
[SECURITY.md](../SECURITY.md) first. Running capsule *itself* in a container
requires handing it the host's Docker socket.

## Your first capsule

From a project directory:

```sh
capsule init
```

That writes a `capsule.toml`, guessing from what it finds. A `go.mod` gives you a
Go image with the module cache persisted, a `package.json` a Node image with the
npm cache, and so on. The file is commented, so open it and adjust the image.

Then:

```sh
capsule up
```

You are now inside a container with your project mounted at `/workspace`. Install
things, break things, experiment. Type `exit` and the container is destroyed,
along with anything you installed into it.

capsule prints what it mounts and what will survive before it starts, every time:

```
capsule: myapp on golang:1-alpine via docker
capsule: mounting /home/m/myapp at /workspace
capsule: on exit only these volumes survive: gomod
```

You do not have to be at the project root. capsule looks for `capsule.toml` in
the current directory and then in each parent, and mounts the directory that
holds it, so `capsule up` from `src/api/handlers` still gives you the whole
project.

## Running one command

`capsule up -- <cmd>` runs a single command in a fresh capsule and tears it down
afterwards. It exits with the command's own status, so it drops into a shell
pipeline or a CI step without wrapping:

```sh
capsule up -- go test ./...
```

Reach for this form when you want a clean toolchain for one job: reproducing a
bug on an older compiler, or checking that your tests pass without whatever is
installed on your machine.

## Seeing what would happen

`capsule up --dry-run` prints the exact runtime command instead of running it:

```sh
capsule up --dry-run
```

The output is a single copy-pasteable line. Use it when a capsule is not behaving
and you want to see the flags, or to check what a `capsule.toml` from a
repository would actually do before you run it.

## When something is wrong

```sh
capsule doctor
```

It checks the runtime, whether the daemon is reachable, that `capsule.toml`
parses, whether the image is already pulled, and which persisted volumes exist.
Each line is `ok`, `note`, or `FAIL`. A `note` is something worth knowing that is
not a problem, such as an image that has not been pulled yet.

## Cleaning up strays

A capsule normally destroys itself. If a terminal was killed or a laptop lid
closed, one can survive:

```sh
capsule list          # what is running
capsule down          # destroy this project's
capsule down --all    # destroy every capsule on the machine
```

capsule keeps no state file. It finds its containers by label, so `down --all`
can always sweep a machine clean, even for projects you no longer have checked
out.
