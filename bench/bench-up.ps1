# `capsule up` cold against warm.
#
# Cold means the image is not on the machine, so the run includes a registry
# pull. Warm means the image is already local and nothing is fetched. These are
# the two numbers a developer actually experiences, and they differ by orders of
# magnitude, so they are never averaged together.
#
#   powershell -ExecutionPolicy Bypass -File bench\bench-up.ps1
#
# Optional: -WarmRuns, -ColdRuns, -Images.

param(
    [int]      $WarmRuns = 30,
    [int]      $WarmWarmup = 3,
    [int]      $ColdRuns = 5,
    [string[]] $Images   = @('alpine:3.20', 'golang:1.25-alpine')
)

. (Join-Path $PSScriptRoot 'lib.ps1')
Initialize-Bench
$capsule = Build-Capsule

# The workload inside the capsule is `true`, so what is measured is the cost of
# getting a container to the point of running something, not the something.
$Work = @('up', '--', 'true')

$samples = @{}
$summaries = @()

foreach ($image in $Images) {
    $dir = New-BenchProject -Image $image
    Write-Host "`n=== $image  (project: $dir) ==="

    # --- cold -------------------------------------------------------------
    # One discarded cold run first. It is not a warmup of the image cache,
    # which is destroyed before every sample, but of everything else the first
    # run of a new binary touches: the PE loader, the OS file cache, and the
    # registry's TLS session. Without it the first sample carries costs that
    # belong to the benchmark rather than to capsule.
    Write-Host "cold: discarding one priming run"
    Remove-Image $image
    $null = Measure-Command-Ms -Exe $capsule -Argv $Work -WorkDir $dir

    $cold = @()
    for ($i = 1; $i -le $ColdRuns; $i++) {
        Remove-Image $image
        if (Test-ImagePresent $image) { throw "image $image survived rmi; a cold run would not be cold" }
        $ms = Measure-Command-Ms -Exe $capsule -Argv $Work -WorkDir $dir
        Write-Host ("  cold run {0}: {1:N0} ms" -f $i, $ms)
        $cold += $ms
    }
    $samples["$image cold"] = $cold
    $summaries += New-Summary "capsule up ($image) cold" $cold

    # --- warm -------------------------------------------------------------
    if (-not (Test-ImagePresent $image)) { $null = Invoke-Docker @('pull', $image) }
    Write-Host "warm: discarding $WarmWarmup warmup runs"
    for ($i = 1; $i -le $WarmWarmup; $i++) {
        $null = Measure-Command-Ms -Exe $capsule -Argv $Work -WorkDir $dir
    }
    $warm = @()
    for ($i = 1; $i -le $WarmRuns; $i++) {
        $warm += Measure-Command-Ms -Exe $capsule -Argv $Work -WorkDir $dir
    }
    Write-Host ("  warm median: {0:N0} ms over {1} runs" -f (Get-Percentile $warm 0.50), $WarmRuns)
    $samples["$image warm"] = $warm
    $summaries += New-Summary "capsule up ($image) warm" $warm

    Remove-Item -Recurse -Force $dir
}

Save-Samples 'up' $samples
Save-Summary 'up' $summaries

