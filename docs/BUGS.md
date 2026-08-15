# Bugs

Defects that got into capsule, how each one was actually caught, and what stops
it coming back. Written down because the interesting part of a bug is rarely the
fix.

The list is short. capsule is about a thousand lines of Go with no dependencies
and it has not been running in production anywhere, so I am not going to dress
this up as a long incident history. What is here is real: every entry names a
commit you can read.

---

## A colon in a mount destination silently changed what got mounted

**Symptom.** A `capsule.toml` with `workdir = "/src:ro"` started a capsule that
appeared to work and had my own project mounted read-only. Nothing said so.
Builds failed inside the capsule with permission errors that pointed at the
project rather than at the config. The same applied to a `[persist]` target:
`cache = "/data:ro"` made the volume read-only without a word.

**Root cause.** A runtime splits a `-v` argument on `:` into
`source:destination:options`. capsule built that argument as
`HostPath(dir) + ":" + c.Workdir` and validated `workdir` only for being an
absolute path. A colon inside it is therefore not part of the path: it ends the
path and turns everything after it into mount options chosen by whoever wrote
the config. `workdir = "/src:ro"` named no directory at all.

This is worse than it first looks because of what capsule is for. The README's
own pitch includes running someone else's repository, which makes `capsule.toml`
an input rather than something I wrote. A config could downgrade the mount of the
reader's own source tree, and the only evidence was a build that failed oddly.

**How it was caught.** `FuzzParseToArgv` in
[`internal/runtime/fuzz_test.go`](../internal/runtime/fuzz_test.go), a Go native
fuzz target that drives `capsule.toml` text through `config.Parse` and then
through every argv builder a `capsule up` uses. It asserts properties rather than
outputs. The one that failed:

```go
// A mount argument is split by the runtime on ":". A workdir or a persist
// target carrying one turns a bind mount into a mount with options the
// capsule.toml author chose, silently.
for i := 0; i < len(args)-1; i++ {
    if args[i] != "-v" {
        continue
    }
    if n := strings.Count(args[i+1], ":"); n > mountColons(args[i+1]) {
        t.Fatalf("mount %q carries more than the src:dst separator", args[i+1])
    }
}
```

The most valuable line is `mountColons`, not the check itself. A Windows host
path keeps its drive-letter colon, so `C:/x:/workspace` is legitimately three
fields, and the naive assertion "a mount has exactly one colon" would have been
false on the platform I develop on. Getting that right is what let the assertion
be strict enough to fail on the real bug.

I would not have written this test case by hand. I was fuzzing for the property I
expected to matter, which was a config-derived argument landing in flag position,
and this came out of the same run.

**Fix.** [`ebd6d5d`](https://github.com/martin-k-m/capsule/commit/ebd6d5d) adds a
`checkNoColon` helper and applies it to `workdir` and to every `[persist]`
target, with an error that explains the splitting rather than just refusing.

**Regression test.** Two cases in `TestParseErrors`, `workdir carries mount
options` and `persist target carries mount options`, plus the fuzz property
itself, which now holds for any document the reader accepts. The three inputs the
fuzzer surfaced are committed as seeds under
`internal/runtime/testdata/fuzz/FuzzParseToArgv/`. Two of them, a `shell` of `-`
and an env key of `-`, turned out to be reachable but harmless; they are seeds so
that staying harmless is checked rather than assumed.

---

## `Runtime.Name()` did not trim a Windows path, but only on Linux

**Symptom.** `TestNameStripsThePathAndExtension` passed on my machine and failed
on CI. Nothing about the test was flaky and nothing about the code was
concurrent.

**Root cause.** `Name()` used `filepath.Base` and `filepath.Ext` to turn
`C:\Program Files\Docker\docker.exe` into `docker`. `path/filepath`'s separator
is fixed at compile time to the OS the code is compiled for, not the OS whose
path is being handled. On Linux, `filepath.Base` treats only `/` as a separator
and a backslash is an ordinary filename character, so a Windows-style path came
back completely untrimmed.

The value being formatted is whatever `exec.LookPath` returned, which depends on
the OS capsule is *running* on. Those are the same OS in production and different
OSes in a cross-platform test, which is exactly why only CI saw it.

**How it was caught.** CI, on the Linux runner, from a test that had been written
on Windows and passed there. This is the bug in the list I am least happy about
and most glad exists, because it is the argument for running the test matrix
somewhere other than the developer's laptop, made concretely.

**Fix.** [`ef41de3`](https://github.com/martin-k-m/capsule/commit/ef41de3)
replaces the `path/filepath` calls with `strings.LastIndexAny(name, "/\\")`, so
both separators are recognised regardless of host, and explains in the doc
comment why `path/filepath` is deliberately not used there.

**Regression test.** `TestNameStripsThePathAndExtension` in
[`internal/runtime/runtime_test.go`](../internal/runtime/runtime_test.go), which
now passes on both platforms rather than one, and CI runs on Linux.

---

## `capsule exec` documented a `--` it never actually emitted

**Symptom.** `capsule exec -- --version` did not reach the program. The runtime's
own `exec` read `--version` as a flag to itself.

**Root cause.** The behaviour was documented and implemented in the argument
parsing, so `capsule exec` correctly stopped reading its own flags at `--`. But
the argv it then built for the runtime was `exec -it <target> <command...>` with
no separator, so the guarantee stopped at capsule's boundary and the runtime made
its own decision about the leading dash.

The general shape of this is a bug I would look for again: a promise that is kept
by the layer that documents it and dropped by the layer below.

**Fix.** [`0966e9a`](https://github.com/martin-k-m/capsule/commit/0966e9a) pulls
the argv into a pure `execCommandArgs` helper and emits the `--`.

**Regression test.** The `command starting with a dash` case in
`TestExecCommandArgs`. Making the argv a pure function is most of the value here:
before that commit there was nothing to assert on without a running container.

---

## A UTF-8 BOM made a correct `capsule.toml` unreadable

**Symptom.**

```
capsule: capsule.toml: unknown key "﻿image"
```

The key is `image`. It is spelled correctly, it is the first line of the file,
and no amount of retyping it helps.

**Root cause.** A file saved by a Windows editor, or written by PowerShell's own
`Set-Content -Encoding utf8`, begins with a UTF-8 byte-order mark. The reader
passed the document through unchanged, so the mark stuck to the first key it
parsed. `\uFEFF` is invisible in every editor and in most terminals, so the error
message names a key that reads as correct.

**How it was caught.** I hit it myself, writing the benchmark harness in
[`bench/`](../bench). `New-BenchProject` generates a `capsule.toml` with
`Set-Content -Encoding utf8`, and the very first benchmark run failed to parse a
five-line config I had just written.

That it took a Windows-side tooling accident to find this is the honest summary.
The Go test suite never produced a BOM because no Go test author types one, and
the fuzzer starts from seeds that do not have one either.

**Fix.** [`810ab4c`](https://github.com/martin-k-m/capsule/commit/810ab4c) strips
a leading `\uFEFF` in `parseTOML`, before any line splitting.

**Regression test.** `TestALeadingByteOrderMarkIsNotPartOfTheFirstKey`, and
`TestAByteOrderMarkIsOnlyStrippedFromTheStart` beside it, because the same rune
anywhere other than the first byte is a zero-width no-break space, which is a
character in a value and has to survive. A fix that trims it everywhere would
pass the first test and silently corrupt data.

---

## `capsule shell` was the one command that could not run without a terminal

**Symptom.** `capsule shell` from a script or a CI step fails with

```
the input device is not a TTY
```

which is an error about the caller's terminal and says nothing about the capsule.

**Root cause.** `runShell` passed `-it` unconditionally. Every other command
gates the TTY on actually having one: `up`, `exec` and `run` all check
`isTerminal(os.Stdin) && isTerminal(os.Stdout)` first, and CLAUDE.md states the
rule as a convention the whole tool follows. `shell` predated the convention and
was never brought in line with it.

**How it was caught.** Auditing every claim in README.md and CLAUDE.md against
the code, rather than by a test. The convention was written down as a fact about
capsule, and one command did not implement it. That is the kind of gap a test
suite does not find, because there was no test asserting the rule anywhere except
in the commands that already followed it.

**Fix.** [`a1b371d`](https://github.com/martin-k-m/capsule/commit/a1b371d) pulls
the argv into a pure `shellCommandArgs`, gates `-t` on the terminal check, and
keeps `-i` on either way so `capsule shell < script.sh` still reaches the shell.

**Regression test.** `TestShellCommandArgsGatesTheTTY` and
`TestShellCommandArgsAlwaysKeepsStdinOpen`.

---

## A test that could only ever pass on one platform

Not a product bug, but the same mistake as the `Runtime.Name()` entry seen from
the other side, and worth keeping next to it.

**Symptom.** `TestHostPathUsesForwardSlashes` asserted that `HostPath` turns a
literal Windows path into a forward-slash one. `filepath.ToSlash` is deliberately
a no-op on Unix, where a backslash is a legal filename character, so the
assertion could only hold on Windows. On Linux it was testing that a no-op is not
a no-op.

**Fix.** [`47d91f1`](https://github.com/martin-k-m/capsule/commit/47d91f1) builds
the input with `filepath.Join` and asserts the contract that actually holds
everywhere: a path built with the host's own separator comes back separated by
forward slashes.

**What I take from it.** Both this and the `Name()` bug are the same error:
reasoning about paths as though the developer's OS were the only one. The rule I
have since applied is that any test touching a path either constructs its input
with `path/filepath` or is explicitly about one platform and says so.

---

## What is not here

I have not had a production incident with this tool, and there is no bug in this
list that a user reported, because as far as I know there are no users yet. Three
of the six entries were found by machinery I set up deliberately (a fuzzer, a
cross-platform CI matrix, an audit of the docs against the code) and one was
found by tripping over it. That ratio is the argument for the machinery.
