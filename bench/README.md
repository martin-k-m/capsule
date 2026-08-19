# bench

The scripts that produce every number in [docs/BENCHMARKS.md](../docs/BENCHMARKS.md)
and every observation in [docs/SANDBOXING.md](../docs/SANDBOXING.md).

They are PowerShell because that is what was actually run, on Windows with
Docker Desktop. The environment they were run in, and the ways it differs from a
Linux host, are recorded in the benchmark document rather than assumed away.

| Script | What it measures |
| :-- | :-- |
| `bench-up.ps1` | `capsule up` cold against warm, for two image sizes |
| `bench-breakdown.ps1` | Where a `capsule up` spends its time, phase by phase |
| `bench-overhead.ps1` | What capsule costs on top of the `docker run` it drives |
| `bench-services.ps1` | What a `[services]` sidecar adds to a `capsule up` |
| `probe-isolation.ps1` | What crosses a capsule's boundary, asked from inside one |
| `lib.ps1` | Timing, percentiles, fixtures. Dot-sourced by the rest |

```powershell
powershell -ExecutionPolicy Bypass -File bench\bench-up.ps1
powershell -ExecutionPolicy Bypass -File bench\bench-breakdown.ps1
powershell -ExecutionPolicy Bypass -File bench\bench-overhead.ps1
powershell -ExecutionPolicy Bypass -File benchench-services.ps1
powershell -ExecutionPolicy Bypass -File bench\bench-services.ps1
powershell -ExecutionPolicy Bypass -File bench\probe-isolation.ps1
```

Each script builds capsule from the working tree first, so it measures the code
you have rather than whatever is on PATH. Results land in `results/`: a
`*-samples.csv` with every individual run, a `*-summary.csv` with the table, and
`isolation.txt` with the raw probe transcript.

They need Docker running and will pull and delete images. `bench-up.ps1` and
`bench-breakdown.ps1` remove `alpine:3.20` and `golang:1.25-alpine` from the
local image store repeatedly, which is the only way a cold measurement is cold.

## Reading the output

`Get-Percentile` is nearest-rank, so every figure is a run that happened. With
the default run counts the p99 column is the maximum rather than an estimated
tail, which is why `n` is printed next to it.

Compare cases within one script's table, never across two. Each script measures
all of its own cases in one interleaved session; a comparison between scripts run
minutes apart is comparing background load.
