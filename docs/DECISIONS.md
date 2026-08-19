# Decisions

The choices in capsule that had a real alternative, and what the alternative
would have cost. Written down because the reasoning is the part that does not
survive in a diff.

## Drive the runtime's CLI, do not talk to its daemon

capsule shells out to `docker` or `podman` and passes them an argv. The
alternative was the Docker Engine API over `/var/run/docker.sock`, which is what
most tools in this space do. It lost because the CLI already carries everything a
developer configured: the active context, registry credentials, mirrors, a
rootless setup. Reimplementing that is work capsule would then own forever, and
it also makes `capsule up --dry-run` an approximation of what capsule does
internally rather than the literal command it runs.

The cost is a process spawn per runtime action. On the machine in
[BENCHMARKS.md](BENCHMARKS.md) the docker CLI needs 232 ms to ask the daemon its
own version, so no capsule can start faster than that. It is the single largest
component of a warm `capsule up`, and it is not capsule's code.

## Keep go.mod empty

No third-party dependencies at all, which meant hand-writing the `capsule.toml`
reader and the terminal detection. A TOML library and a CLI framework would have
been two lines in `go.mod` and are both better than what is here.

They lost on what capsule is: a program whose entire job is to build a privileged
`docker run` argv out of a file. Every dependency in that path is code that gets
to influence what is mounted and what is exposed, and reviewing a 270-line reader
is a thing a person can actually do in one sitting.

The cost is real and has been paid. The hand-rolled reader accepts a subset of
TOML rather than the language, and it shipped with a bug a mature library would
not have had: it did not strip a UTF-8 byte-order mark, so a `capsule.toml` saved
by a Windows editor was rejected with an error naming a key that looked correct
(see [BUGS.md](BUGS.md)).

## A config file, not flags

The environment could have been described on the command line:
`capsule run --image golang:1-alpine --mount . --port 8080:8080`. That version is
easier to try once and useless the second time, because the description lives in
a shell history instead of in the project.

`capsule.toml` is checked in, so the environment is a property of the repository
and a colleague gets the same one. It is also what makes `capsule init` worth
having: a file can be generated, inspected and edited, and an argv cannot.

## Treat capsule.toml as untrusted input

The README's own pitch includes running someone else's repository. That single
sentence decides the security posture: the file that describes the container is
not necessarily written by the person the container will run as.

The alternative, treating it as trusted because it is in your working directory,
is the ordinary assumption and would have removed a layer of validation. It lost
because the failure mode is silent. `image` is checked for a leading `-` so it
cannot become a flag, mount destinations are checked for `:` so they cannot
become mount options, and `FuzzParseToArgv` asserts those properties for any
document the reader accepts rather than for the ones a test author thought of. It
found the colon bug.

## Never write a state file

capsule stamps `me.blinkdev.capsule` on everything it creates and finds its own
objects by querying that label. The alternative was the obvious one: a
`~/.capsule/state.json` listing running capsules.

A state file can be wrong. Kill a terminal, reboot mid-session, `docker rm`
something by hand, and the file disagrees with the machine while a label query
still does not. There is nothing capsule needs to remember that the containers do
not already know, so the file would have been a second source of truth with no
compensating benefit.

## `--rm` instead of a teardown step

Ephemerality is the product, so it is structural rather than procedural. The
container is started with `--rm`, which means the runtime removes it when the
process ends, whatever ended it.

A teardown step, a `defer` or a signal handler that removes the container, would
have been more flexible and is what a first draft looks like. It lost because
every one of those paths can be skipped: SIGKILL, a panic, a machine losing
power. `TestRunArgsIsAlwaysEphemeral` pins `--rm` as the second argument so the
guarantee cannot be removed quietly.

## Whatever runtime is on PATH, rather than one runtime or none

`Detect()` prefers `docker`, falls back to `podman`, and says so when neither is
present. Two alternatives were considered and both lost.

Committing to Docker alone would have simplified the argv, since podman
compatibility constrains which flags capsule can use. It lost because podman
users are exactly the users who care most about a disposable environment, and the
overlap in the flags capsule actually needs turned out to be total.

Going lower, driving `runc` or containerd directly, would have cut the per-call
CLI cost that dominates the measured warm start. It lost decisively: image pull,
layer storage, network setup and volume management are the parts capsule would
then have to implement, and they are most of what a container runtime is.

## A capsule is root, on an open network, by default

This is the compromise, and it is a real one. A capsule runs as uid 0 with
Docker's default capability set and unrestricted outbound networking. `--user`,
`--cap-drop=ALL` and a default-deny network were all available and all rejected.

They lost to what a development environment has to be able to do. `packages`
installs with the image's package manager, which needs root and needs the
network; a Go build needs the module proxy; the whole point of `[services]` is
that the capsule talks to them. A capsule that cannot install a package or reach
a registry is not a development environment.

The cost is that a capsule is a clean room, not a sandbox. What that means
exactly was measured rather than asserted: see
[SANDBOXING.md](SANDBOXING.md) for what crosses the boundary and what does not.
The README says a capsule is "not a sandbox for untrusted code", and that
sentence is load-bearing.

## The signal handler is tested in a real process, not around a seam

`trap` calls `os.Exit` from a goroutine, which is the right thing for it to do
and untestable in the usual way. The alternative was an injectable exit
function, or an interface in front of `Runtime` so the whole thing could be
driven in memory.

Both lost to the same objection: they test a rearranged version of the code,
and the arrangement is what signal handling gets wrong. What is here instead
re-executes the test binary twice. Once as a capsule that has installed a trap,
which is then really signalled and whose real exit status is read; once as the
container runtime itself, which records the commands it was asked to run and
fails the exact one a test names. Nothing in `services.go` changed to make it
testable.

The cost is that the two signal tests skip on Windows, where there is no POSIX
signal to send to another process. That is a real hole on the machine capsule is
developed on, and the reason CI is the platform that matters for those two.
