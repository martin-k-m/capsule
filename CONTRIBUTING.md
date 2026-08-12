# Contributing to capsule

Thanks for taking a look. capsule drives a container runtime rather than
replacing one, and the design promise is that every action it takes is one the
developer could have typed by hand. Changes should keep that true.

## Setup

You need Go (the CI uses the current stable release) and either Docker or
Podman available for the tests and smoke checks that touch a runtime.

```bash
git clone https://github.com/martin-k-m/capsule
cd capsule
go build -o capsule ./cmd/capsule
go test ./... -race
./capsule doctor        # checks a runtime is reachable
```

## Ground rules

- **capsule shells out; it does not talk to a daemon socket.** It runs `docker`
  or `podman` so it inherits the developer's existing context and credentials.
  A change that reaches for a runtime API instead of the CLI breaks that
  principle and needs a strong reason.
- **The source outlives the capsule.** `capsule up` must never put project
  source anywhere that a teardown could destroy. Anything meant to persist is
  named under `[persist]`, and nothing else survives.
- **Errors are actionable.** `capsule doctor` is the model: say what is wrong
  and what to do about it, not just that something failed.
- **Behaviour changes come with tests.** The suite lives under `internal/` and
  runs with `-race`; add to the package you touched.

## Before you open a pull request

CI gates on all of these, so run them locally first:

```bash
gofmt -l .              # must print nothing
go vet ./...
go test ./... -race
go build ./cmd/capsule
```

Keep pull requests focused. The commit history favours small, self-describing
changes, and that is the style to match.

## Reporting bugs

Open an issue with your `capsule.toml`, the exact command, the runtime you use
(`docker` or `podman`) and its version, and the output of `capsule doctor`.
That last one resolves most environment questions before they need a round trip.
