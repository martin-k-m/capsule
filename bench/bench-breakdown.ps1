# Where a `capsule up` actually spends its time.
#
# The cold and warm totals in bench-up.ps1 say what a developer waits for. This
# script says what they are waiting on, by timing the same work as separate
# runtime calls:
#
#   capsule's own work   capsule up --dry-run: find the config, parse and
#                        validate it, detect the runtime, build the argv.
#                        Everything except the runtime call.
#   image pull           docker pull, with the image removed first.
#   container create     docker create, with capsule's own flags.
#   start + command      docker start -a, which runs the container to exit.
#                        The command is `true`, so this is start, not workload.
#   remove               docker rm.
#
# The phases are timed as separate processes, so each one carries its own docker
# CLI startup and the sum is larger than the single `docker run` that does all
# of it at once. That gap is reported rather than hidden: see BENCHMARKS.md.
#
#   powershell -ExecutionPolicy Bypass -File bench\bench-breakdown.ps1

param(
    [int]    $Runs      = 20,
    [int]    $ColdRuns  = 5,
    [int]    $Warmup    = 3,
    [string] $Image     = 'alpine:3.20'
)

. (Join-Path $PSScriptRoot 'lib.ps1')
Initialize-Bench
$capsule   = Build-Capsule
$dockerExe = (Get-Command docker).Source
$dir       = New-BenchProject -Image $Image
$hostPath  = (Resolve-Path $dir).Path.Replace('\', '/')
$cname     = 'capsule-bench-breakdown'

# The same flags capsule emits, as a create rather than a run. `--rm` is dropped
# because the removal is being timed as its own phase.
$createArgv = @(
    'create',
    '--name', $cname,
    '--hostname', 'bench',
    '--label', 'me.blinkdev.capsule=1',
    '--label', 'me.blinkdev.capsule.name=bench',
    '--label', 'me.blinkdev.capsule.role=capsule',
    '-v', "${hostPath}:/workspace",
    '-w', '/workspace',
    $Image,
    '/bin/sh', '-c', "set -e; exec 'true'"
)

$samples   = @{}
$summaries = @()

# --- the docker CLI's own floor -------------------------------------------
#
# The cheapest question that still requires a full round trip to the daemon.
# Nothing about a capsule can cost less than this, because capsule reaches the
# runtime by running its CLI, and on Windows that CLI talks to a daemon inside a
# Linux VM. Measuring it turns the rest of the table from a list of durations
# into an account of what they are made of.
$floor = @()
for ($i = 1; $i -le $Warmup; $i++) { $null = Measure-Command-Ms -Exe $dockerExe -Argv @('version', '--format', '{{.Server.Version}}') }
for ($i = 1; $i -le $Runs; $i++) { $floor += Measure-Command-Ms -Exe $dockerExe -Argv @('version', '--format', '{{.Server.Version}}') }
$samples['0 docker CLI floor (docker version)'] = $floor
$summaries += New-Summary '0 docker CLI floor (docker version)' $floor

# --- capsule's own work ----------------------------------------------------
for ($i = 1; $i -le $Warmup; $i++) { $null = Measure-Command-Ms -Exe $capsule -Argv @('up', '--dry-run', '--', 'true') -WorkDir $dir }
$own = @()
for ($i = 1; $i -le $Runs; $i++) { $own += Measure-Command-Ms -Exe $capsule -Argv @('up', '--dry-run', '--', 'true') -WorkDir $dir }
$samples['1 capsule own work (dry-run)'] = $own
$summaries += New-Summary '1 capsule own work (dry-run)' $own

# --- image pull, cold ------------------------------------------------------
# Every sample removes the image first, so every one of them is a real pull.
Remove-Image $Image
$null = Measure-Command-Ms -Exe $dockerExe -Argv @('pull', $Image)   # priming run, discarded
$pull = @()
for ($i = 1; $i -le $ColdRuns; $i++) {
    Remove-Image $Image
    if (Test-ImagePresent $Image) { throw "image $Image survived rmi" }
    $pull += Measure-Command-Ms -Exe $dockerExe -Argv @('pull', $Image)
}
$samples['2 image pull (cold)'] = $pull
$summaries += New-Summary '2 image pull (cold)' $pull

# --- pull with the image already present -----------------------------------
# What the same command costs when there is nothing to fetch. This is the part
# of a cold pull that is not bytes over the wire.
$pullWarm = @()
for ($i = 1; $i -le $Warmup; $i++) { $null = Measure-Command-Ms -Exe $dockerExe -Argv @('pull', $Image) }
for ($i = 1; $i -le $Runs; $i++) { $pullWarm += Measure-Command-Ms -Exe $dockerExe -Argv @('pull', $Image) }
$samples['3 image pull (already present)'] = $pullWarm
$summaries += New-Summary '3 image pull (already present)' $pullWarm

# --- create / start / remove ----------------------------------------------
$create = @(); $start = @(); $remove = @()
for ($i = 1; $i -le ($Runs + $Warmup); $i++) {
    $null = Invoke-Docker @('rm', '-f', $cname)   # in case a previous sample died mid-cycle
    $c = Measure-Command-Ms -Exe $dockerExe -Argv $createArgv
    $s = Measure-Command-Ms -Exe $dockerExe -Argv @('start', '-a', $cname)
    $r = Measure-Command-Ms -Exe $dockerExe -Argv @('rm', $cname)
    if ($i -gt $Warmup) { $create += $c; $start += $s; $remove += $r }
}
$samples['4 container create'] = $create
$samples['5 start + command']  = $start
$samples['6 container remove'] = $remove
$summaries += New-Summary '4 container create' $create
$summaries += New-Summary '5 start + command'  $start
$summaries += New-Summary '6 container remove' $remove

# --- the whole thing as one docker run, for comparison ---------------------
$runArgv = @('run', '--rm', '-v', "${hostPath}:/workspace", '-w', '/workspace', $Image, '/bin/sh', '-c', "set -e; exec 'true'")
$run = @()
for ($i = 1; $i -le $Warmup; $i++) { $null = Measure-Command-Ms -Exe $dockerExe -Argv $runArgv }
for ($i = 1; $i -le $Runs; $i++) { $run += Measure-Command-Ms -Exe $dockerExe -Argv $runArgv }
$samples['7 docker run --rm (all of 4-6 in one call)'] = $run
$summaries += New-Summary '7 docker run --rm (all of 4-6 in one call)' $run

Save-Samples 'breakdown' $samples
Save-Summary 'breakdown' $summaries
Remove-Item -Recurse -Force $dir
