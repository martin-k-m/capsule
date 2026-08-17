# What `[services]` costs.
#
# BENCHMARKS.md predicted that a capsule with sidecars would be dominated by the
# number of runtime invocations it makes, and said plainly that the prediction
# was not a number. This is the number.
#
# Four cases against warm images, measured in one interleaved session so they
# are comparable to each other and to nothing else:
#
#   0 services                  the baseline `capsule up -- true`
#   1 service, no `ready`       adds network create, run, inspect, 2x rm, network rm
#   1 service, with `ready`     adds the readiness probe and its 500 ms poll
#   2 services, no `ready`      adds a second run, inspect and rm on top of that
#
#   powershell -ExecutionPolicy Bypass -File bench\bench-services.ps1
#
# Optional: -Runs, -Warmup.

param(
    [int] $Runs   = 10,
    [int] $Warmup = 2
)

. (Join-Path $PSScriptRoot 'lib.ps1')
Initialize-Bench
$capsule = Build-Capsule

$base    = 'alpine:3.20'
$service = 'redis:7-alpine'

foreach ($image in @($base, $service)) {
    if (-not (Test-ImagePresent $image)) {
        Write-Host "pulling $image"
        $null = Invoke-Docker @('pull', $image)
    }
}

# The workload is `true`, as everywhere else here, so what is measured is the
# cost of getting to the point of running something.
$Work = @('up', '--', 'true')

function New-ServicesProject {
    param([AllowEmptyString()][string] $Services)
    $dir = Join-Path ([System.IO.Path]::GetTempPath()) ("capsule-bench-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $dir | Out-Null
    $toml = @"
capsule = ">=0.2"
name    = "svcbench"
image   = "$base"
shell   = "/bin/sh"
workdir = "/workspace"
$Services
"@
    [System.IO.File]::WriteAllText(
        (Join-Path $dir 'capsule.toml'), $toml,
        (New-Object System.Text.UTF8Encoding($false)))
    return $dir
}

$cases = [ordered]@{
    'no services' = ''
    '1 service, no ready check' = @"

[services.cache]
image = "$service"
"@
    '1 service, with ready check' = @"

[services.cache]
image = "$service"
ready = "redis-cli ping"
"@
    '2 services, no ready check' = @"

[services.cache]
image = "$service"

[services.queue]
image = "$service"
"@
}

$samples = @{}
$summaries = @()

foreach ($name in $cases.Keys) {
    $dir = New-ServicesProject -Services $cases[$name]
    Write-Host "`n=== $name  (project: $dir) ==="

    for ($i = 1; $i -le $Warmup; $i++) {
        $null = Measure-Command-Ms -Exe $capsule -Argv $Work -WorkDir $dir
    }
    $ms = @()
    for ($i = 1; $i -le $Runs; $i++) {
        $ms += Measure-Command-Ms -Exe $capsule -Argv $Work -WorkDir $dir
    }
    Write-Host ("  median: {0:N0} ms over {1} runs" -f (Get-Percentile $ms 0.50), $Runs)

    # A leaked service container would make the next case's numbers someone
    # else's, so it is checked rather than assumed.
    $left = (Invoke-Docker @('ps', '-aq', '--filter', 'label=me.blinkdev.capsule=1')).out
    if ($left.Trim() -ne '') { throw "capsule left containers behind: $left" }

    $samples[$name] = $ms
    $summaries += New-Summary $name $ms
    Remove-Item -Recurse -Force $dir
}

Save-Samples 'services' $samples
Save-Summary 'services' $summaries
