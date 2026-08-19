# Benchmarks

What a capsule costs, measured. Every number on this page came from a script in
[`bench/`](../bench), run on the machine described below, with the raw per-run
samples committed alongside the summaries in
[`bench/results/`](../bench/results).

Nothing here is estimated, extrapolated, or carried over from a previous run.

## Environment

| | |
| :-- | :-- |
| CPU | Intel Core Ultra 9 285H, 16 physical cores, 16 logical, 2.90 GHz nominal |
| RAM | 16,573,108,224 bytes (15.4 GiB) |
| Storage | NVMe SSD, Timetec 35TT2280GEN4P-2TB |
| Machine | ASUS ROG Zephyrus G16 GU605CP, on mains power |
| OS | Windows 11 Pro, build 26200 (10.0.26200) |
| Go | go1.26.5 windows/amd64 |
| Container runtime | Docker 29.6.2, client and server |
| Docker Desktop | 4.83.0.234302 |
| Docker server OS | `linux/amd64`, storage driver `overlayfs`, cgroup v2 |
| Linux VM | WSL 2.7.11.0, kernel `6.18.33.2-microsoft-standard-WSL2` |
| VM resources | 4 CPUs, 6,214,496,256 bytes (5.79 GiB) visible to the daemon |
| capsule | built from `harden/evidence` by each script, via `go build` |
| Date | 2026-08-15 |

### The caveat that matters most

**Docker on Windows is not Docker on Linux.** The daemon runs inside a WSL2
Linux VM. Every `docker` command crosses from a Windows process into that VM, and
the project directory reaches the container over a 9p filesystem rather than a
native bind mount. Both add latency that a native Linux host does not pay.

Concretely: the floor measured below, 225 ms for the docker CLI to ask the daemon
its own version, is dominated by that crossing. On native Linux the same question
is normally answered in tens of milliseconds. **Absolute timings here are
therefore an upper bound, and the ratios are the transferable part.** capsule's
own share of the work is native Windows code and is not affected.

### Was the machine idle?

**No, and it should be said.** These benchmarks were run as part of an
agent-driven session that was also editing files, running `go test` and holding a
language server open. Nothing else heavy was running, the machine was on mains
power, and no other container workload was active, but this is a working laptop
rather than a quiet benchmark rig.

The effect is visible in the data and is not hidden: the same command,
`capsule up -- true` against a warm `alpine:3.20`, has a median of **1010 ms** in
`bench-up.ps1` and **539 ms** in `bench-overhead.ps1`. Same binary, same machine,
same day, different background load.

That is the single most important methodological point on this page, so it gets
its own rule:

> **Compare numbers within one table, never across tables.** Each script measures
> all of its own cases in one interleaved session, so a comparison inside a table
> is valid. A comparison between tables is not.

## Method

- **Timing.** A .NET `Stopwatch` brackets the whole child process: spawn, run,
  exit. That is what a developer waits for. Output is drained into strings rather
  than piped through PowerShell, which would add its own marshalling cost.
- **Warmup.** Three discarded runs before every warm case. The cold cases get one
  discarded priming run, which warms the PE loader, the OS file cache and the
  registry TLS session, but not the image cache: that is destroyed before every
  cold sample and the script asserts the image is really gone before timing.
- **Run counts.** 30 for warm cases, 20 for the phase breakdown, 5 for anything
  that pulls an image over the network.
- **Percentiles.** Nearest-rank, so every figure printed is a run that actually
  happened. **With n = 30 the p99 is the maximum**, and with n = 5 or 20 it is
  also the maximum. Those columns are the observed worst case, not an estimate of
  a tail, and the `n` column is printed beside them for exactly this reason.
- **Median, not mean.** A single scheduling hiccup moves a mean and does not move
  a median. Both the median and the observed spread are given.

Reproduce all of it:

```powershell
powershell -ExecutionPolicy Bypass -File bench\bench-up.ps1
powershell -ExecutionPolicy Bypass -File bench\bench-breakdown.ps1
powershell -ExecutionPolicy Bypass -File bench\bench-overhead.ps1
```

Each writes a `*-samples.csv` with every individual run and a `*-summary.csv`
with the table.

---

## 1. `capsule up`, cold against warm

Cold means the image is not on the machine and the run includes a registry pull.
Warm means it is already local. The workload inside the capsule is `true`, so
what is being measured is the cost of getting to the point of running something.

`bench\bench-up.ps1`

| Case | n | min | **p50** | p90 | **p99 (= max)** |
| :-- | --: | --: | --: | --: | --: |
| `alpine:3.20` cold | 5 | 5,019 ms | **6,158 ms** | 6,544 ms | 6,544 ms |
| `alpine:3.20` warm | 30 | 791 ms | **1,010 ms** | 1,511 ms | 1,795 ms |
| `golang:1.25-alpine` cold | 5 | 12,614 ms | **14,121 ms** | 15,284 ms | 15,284 ms |
| `golang:1.25-alpine` warm | 30 | 720 ms | **968 ms** | 1,199 ms | 1,453 ms |

Image sizes, for reading the cold numbers: `alpine:3.20` is 3.6 MB unpacked,
`golang:1.25-alpine` is 64.4 MB.

**Cold costs 6× warm for a tiny image and 15× for a modest one.** That contrast
is the whole point of the table. A first `capsule up` on a new image is a
coffee-length wait; every one after it is not.

**Warm start does not depend on image size.** 1,010 ms for a 3.6 MB image and
968 ms for a 64.4 MB one, with the larger image nominally faster. The difference
is inside the run-to-run noise, which is the finding: once the layers are local,
starting a container from a big image costs the same as starting one from a small
image. Nothing is copied per start.

**The cold p99 is not a tail estimate.** With n = 5 it is the slowest of five
pulls. Cold numbers also depend entirely on the link to the registry, so they are
the least transferable figures on this page. The ratio to warm is the durable
part; the absolute seconds are a property of this connection on this afternoon.

---

## 2. Where the time actually goes

The same work, timed as separate runtime calls.

`bench\bench-breakdown.ps1`, `alpine:3.20`

| Phase | n | min | **p50** | p90 | **p99 (= max)** |
| :-- | --: | --: | --: | --: | --: |
| 0. docker CLI floor (`docker version`) | 20 | 178 ms | **226 ms** | 249 ms | 270 ms |
| 1. capsule's own work (`up --dry-run`) | 20 | 12 ms | **21 ms** | 34 ms | 72 ms |
| 2. image pull, cold | 5 | 4,273 ms | **5,234 ms** | 6,450 ms | 6,450 ms |
| 3. image pull, already present | 20 | 2,331 ms | **2,593 ms** | 3,205 ms | 3,643 ms |
| 4. container create | 20 | 220 ms | **373 ms** | 905 ms | 2,277 ms |
| 5. start + command | 20 | 447 ms | **840 ms** | 1,419 ms | 1,609 ms |
| 6. container remove | 20 | 185 ms | **241 ms** | 488 ms | 564 ms |
| 7. all of 4–6 as one `docker run --rm` | 20 | 502 ms | **714 ms** | 1,221 ms | 1,438 ms |

### Reading it

**capsule's own work is 21 ms.** Finding `capsule.toml` by walking up the tree,
parsing it, validating it, detecting the runtime and building the argv costs
about a fiftieth of a second. Everything else on this page is the container
runtime.

**The docker CLI floor is 226 ms and nothing can go below it.** That is the cost
of one `docker` process starting on Windows and getting an answer from a daemon
inside a Linux VM, for the cheapest question there is. It is more than ten times
capsule's entire share of the work.

**Two thirds of a "cold pull" is not bytes.** Pulling an image that is *already
present* still costs 2,593 ms, because the CLI contacts the registry and checks
the manifest digest before concluding there is nothing to fetch. Against a cold
pull of 5,234 ms for a 3.6 MB image, that means roughly half the cold time is
round trips and manifest work rather than transfer. For a small image, the
registry conversation dominates the download.

**The phases sum to more than the whole, and the gap is exactly the CLI.**
373 + 840 + 241 = 1,454 ms as three separate commands, against 714 ms when
`docker run --rm` does all three in one call. The 740 ms difference is very close
to three extra CLI startups at 226 ms each (678 ms). The model holds, which is
the useful part: **on this platform, the number of runtime invocations matters
more than what each one does.**

That is also the strongest argument for a design decision capsule already made.
A capsule with no services issues exactly one runtime command. Splitting `up`
into pull-then-create-then-start, which would give nicer progress reporting,
would cost roughly 450 ms per capsule for the reporting alone.

---

## 3. What capsule costs you

The honest question: if capsule were not there and you typed the `docker run`
yourself, how much faster would it be?

All four cases were measured in one interleaved session against a warm image, so
these are directly comparable to each other.

`bench\bench-overhead.ps1`, `alpine:3.20`

| Case | n | min | **p50** | p90 | **p99 (= max)** |
| :-- | --: | --: | --: | --: | --: |
| `capsule up -- true` | 30 | 442 ms | **539 ms** | 645 ms | 703 ms |
| `docker run` with capsule's own argv, by hand | 30 | 431 ms | **528 ms** | 598 ms | 682 ms |
| `docker run`, minimal hand-typed equivalent | 30 | 469 ms | **531 ms** | 592 ms | 805 ms |
| `capsule up --dry-run`, runtime call removed | 30 | 11 ms | **12 ms** | 25 ms | 34 ms |

### capsule costs 11 ms

**539 ms against 528 ms: capsule adds 11 ms at the median, about 2%.** That is
the number to quote. It is what one extra process, a config read and an argv
build cost on top of the `docker run` that was going to happen anyway.

The baseline is not something written by hand and hoped to be equivalent. The
script takes it from `capsule up --dry-run`, which prints the command capsule
would run, reconstructs the argv, and **asserts the reconstruction renders
identically to what capsule printed** before timing anything. If `RunArgs` ever
changes, the benchmark fails rather than quietly comparing two different things.

**capsule's extra flags are free.** The minimal hand-typed version, with no
labels, no hostname and no shell wrapper, comes out at 531 ms against 528 ms for
capsule's fuller argv. Four labels, a `--hostname` and a `sh -c` wrapper cost
nothing measurable. The runtime does not charge per flag.

**Removing the runtime call removes 98% of the time.** `capsule up --dry-run`
does everything except talk to docker and takes 12 ms. A `capsule up` is 12 ms of
capsule and roughly 527 ms of container runtime.

---

## 4. What `[services]` costs

Section 2 predicted that a capsule with sidecars would be dominated by the
number of runtime invocations it makes, and said the prediction was not a
number. It is now.

Measured 2026-08-17 on the machine above, Docker 29.6.2, from branch
`test/cli-teardown-signals`. The rest of this page is from 2026-08-15, so these
four cases compare to each other and to nothing else on it.

`bench\bench-services.ps1`, capsule on `alpine:3.20`, services on `redis:7-alpine`,
all images warm, all four cases in one interleaved session

| Case | n | min | **p50** | p90 | **p99 (= max)** |
| :-- | --: | --: | --: | --: | --: |
| no services | 10 | 498 ms | **578 ms** | 683 ms | 825 ms |
| 1 service, no `ready` check | 10 | 1,996 ms | **2,435 ms** | 2,718 ms | 3,171 ms |
| 1 service, with `ready` check | 10 | 2,496 ms | **2,696 ms** | 2,897 ms | 3,012 ms |
| 2 services, no `ready` check | 10 | 2,691 ms | **3,695 ms** | 4,934 ms | 5,033 ms |

### Reading it

**One service costs 1,857 ms, and it is invocation count, not the service.**
A capsule with no services issues one runtime command. One service turns that
into seven: network create, service run, one state inspect, the capsule's own
run, two removals and a network remove. Six extra invocations for 1,857 ms is
310 ms each, against the 226 ms CLI floor measured in section 2 plus whatever
each command actually does. The prediction holds.

**A second service costs 1,260 ms for three more invocations**, 420 ms each.
More than the first service's per-call figure, and the p90 spread of that case
is the widest on the page, so this is the case where the machine's own load
shows through most. The shape is right; the exact figure is the softest number
here.

**A `ready` check costs one probe, not a poll cycle.** 2,696 ms against
2,435 ms is 261 ms, one more `docker exec`. `readyPoll` is 500 ms, and a redis
that answers on the first probe never waits it out. A service that is slower
than its first probe pays 500 ms of granularity per poll on top, which this
table does not measure and no fast service pays.

**The floor for a services capsule on this platform is about two seconds**, and
almost none of it is capsule. That is the price of a container runtime CLI being
asked seven questions instead of one.

---

## Summary

| Question | Answer, on this machine |
| :-- | :-- |
| What does capsule add over `docker run`? | 11 ms, about 2% |
| What does capsule's own logic cost? | 12 to 21 ms |
| What does one `[services]` sidecar add? | 1,857 ms, six extra runtime invocations |
| What does a warm `capsule up` cost? | 0.5 to 1.0 s, depending on machine load |
| What does a cold one cost? | 6 s for a 3.6 MB image, 14 s for a 64 MB one |
| Does warm start depend on image size? | No |
| What dominates a warm start? | The docker CLI reaching a daemon in a VM: 226 ms floor |
| What dominates a cold start? | The registry. Half of it is round trips, not bytes |

The tool is not the cost. On this platform capsule is a 2% tax on a container
runtime that itself cannot start anything in under a fifth of a second.

## What has not been measured

Stated so the gaps are not mistaken for zeros.

- **Native Linux.** Everything here is Windows with Docker Desktop. The
  per-invocation CLI cost, which is the dominant term, should be substantially
  lower on Linux, and the ratios in section 2 would shift accordingly. Nobody
  should quote these absolute numbers as capsule's performance on a Linux host.
- **Podman.** capsule supports it and it was not benchmarked. Podman has no
  long-running daemon, so its per-invocation cost has a different shape
  entirely. It is not installed on the machine described above, and measuring
  it anywhere else would break the rule that every number here comes from one
  environment.
- **A service slower than its first `ready` probe.** Section 4 measures a redis
  that answers immediately. The 500 ms poll granularity a slow service pays is
  visible in the code and not in these numbers.
- **Anything under sustained load.** No case here runs concurrent capsules or
  competes for I/O beyond whatever the machine happened to be doing.
