# What capsule costs on top of the `docker run` it drives.
#
# capsule shells out to a container runtime, so its overhead is a real number
# that can be isolated: run the capsule, then run the exact command the capsule
# would have run, and subtract. Four cases, warm image throughout, so nothing
# here is measuring a registry.
#
#   1. capsule up -- true        the tool
#   2. docker run (capsule argv) the command capsule builds, typed by hand
#   3. docker run (minimal)      what a developer would have typed instead
#   4. capsule up --dry-run      capsule with the runtime call removed
#
#   powershell -ExecutionPolicy Bypass -File bench\bench-overhead.ps1

param(
    [int]    $Runs   = 30,
    [int]    $Warmup = 3,
    [string] $Image  = 'alpine:3.20'
)

. (Join-Path $PSScriptRoot 'lib.ps1')
Initialize-Bench
$capsule = Build-Capsule

$dir = New-BenchProject -Image $Image
if (-not (Test-ImagePresent $Image)) { $null = Invoke-Docker @('pull', $Image) }

# --- derive the baseline argv from capsule itself --------------------------
#
# The hand-typed baseline is not written out here. It is taken from
# `capsule up --dry-run`, which prints the command capsule would run, so the
# comparison cannot drift into measuring capsule against something capsule does
# not actually do. The rendered form is re-checked below.
$dry = Measure-Command-Ms -Exe $capsule -Argv @('up', '--dry-run', '--', 'true') -WorkDir $dir
$plan = (Invoke-Docker @('version', '--format', '{{.Client.Version}}'))  # touch docker once so it is warm
$null = $plan

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $capsule
$psi.Arguments = ConvertTo-WindowsArgLine @('up', '--dry-run', '--', 'true')
$psi.WorkingDirectory = $dir
$psi.RedirectStandardOutput = $true
$psi.UseShellExecute = $false
$p = [System.Diagnostics.Process]::Start($psi)
$dryLine = $p.StandardOutput.ReadToEnd().Trim()
$p.WaitForExit()
if ($p.ExitCode -ne 0) { throw "capsule up --dry-run failed" }
Write-Host "capsule would run:`n  $dryLine`n"

$host_path = (Resolve-Path $dir).Path.Replace('\', '/')

# capsule's own argv, reconstructed. Every element is fixed except the id in
# --name, which capsule randomises per run; a fixed one is fine here because
# --rm removes the container before the next sample starts.
$capsuleArgv = @(
    'run', '--rm',
    '--name', 'capsule-bench-benchmark',
    '--hostname', 'bench',
    '--label', 'me.blinkdev.capsule=1',
    '--label', 'me.blinkdev.capsule.name=bench',
    '--label', 'me.blinkdev.capsule.role=capsule',
    '-v', "${host_path}:/workspace",
    '-w', '/workspace',
    $Image,
    '/bin/sh', '-c', "set -e; exec 'true'"
)

# Assert the reconstruction against what capsule printed, with the random id
# normalised out. If RunArgs ever changes, this baseline stops being the same
# command and the benchmark says so instead of quietly comparing two things.
$rendered = 'docker ' + (($capsuleArgv | ForEach-Object { Format-ShellWord $_ }) -join ' ')
# Only the --name value is normalised. The project directory happens to carry
# the same prefix, and rewriting it too would hide a real mismatch in the mount.
$normalise = { param($s) [regex]::Replace($s, '--name \S+', '--name NAME') }
if ((& $normalise $rendered) -ne (& $normalise $dryLine)) {
    throw "reconstructed baseline does not match capsule's own plan:`n  built: $rendered`n  plan:  $dryLine"
}
Write-Host "baseline argv verified against capsule up --dry-run`n"

# The command a developer would type if capsule did not exist: the mount, the
# working directory, the image, ephemeral. No labels, no hostname, no shell
# wrapper. The difference between this and the case above is what capsule's
# extra flags cost, separate from what the capsule process costs.
$minimalArgv = @('run', '--rm', '-v', "${host_path}:/workspace", '-w', '/workspace', $Image, 'true')

$dockerExe = (Get-Command docker).Source

$cases = [ordered]@{
    'capsule up -- true'                  = @{ exe = $capsule;   argv = @('up', '--', 'true');            wd = $dir }
    'docker run, capsule argv, by hand'   = @{ exe = $dockerExe; argv = $capsuleArgv;                     wd = $dir }
    'docker run, minimal equivalent'      = @{ exe = $dockerExe; argv = $minimalArgv;                     wd = $dir }
    'capsule up --dry-run (no runtime)'   = @{ exe = $capsule;   argv = @('up', '--dry-run', '--', 'true'); wd = $dir }
}

$samples   = @{}
$summaries = @()
foreach ($name in $cases.Keys) {
    $c = $cases[$name]
    for ($i = 1; $i -le $Warmup; $i++) { $null = Measure-Command-Ms -Exe $c.exe -Argv $c.argv -WorkDir $c.wd }
    $ms = @()
    for ($i = 1; $i -le $Runs; $i++) { $ms += Measure-Command-Ms -Exe $c.exe -Argv $c.argv -WorkDir $c.wd }
    Write-Host ("{0,-38} median {1,7:N1} ms" -f $name, (Get-Percentile $ms 0.50))
    $samples[$name] = $ms
    $summaries += New-Summary $name $ms
}

Save-Samples 'overhead' $samples
Save-Summary 'overhead' $summaries
Remove-Item -Recurse -Force $dir
