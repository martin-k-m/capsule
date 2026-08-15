# Shared helpers for capsule's benchmarks.
#
# Every number in docs/BENCHMARKS.md comes from one of the scripts that dot-
# sources this file. Nothing here estimates: each sample is one wall-clock
# measurement of one process, recorded individually so the raw CSV can be
# re-read and the summary recomputed.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Repo root, so a script can be run from anywhere.
$script:RepoRoot = Split-Path -Parent $PSScriptRoot
$script:OutDir   = Join-Path $PSScriptRoot 'results'

function Initialize-Bench {
    if (-not (Test-Path $script:OutDir)) {
        New-Item -ItemType Directory -Path $script:OutDir | Out-Null
    }
}

# Build the capsule binary under test. Benchmarks measure the working tree, not
# whatever happens to be on PATH.
function Build-Capsule {
    $exe = Join-Path $script:OutDir 'capsule.exe'
    Push-Location $script:RepoRoot
    try {
        & go build -o $exe ./cmd/capsule
        if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
    return $exe
}

# Join an argv into the single command-line string Windows actually passes,
# quoting by the rule CommandLineToArgvW parses back: backslashes are doubled
# only where they precede the closing quote, and a literal quote is escaped.
function ConvertTo-WindowsArgLine {
    param([string[]] $Argv)
    $out = foreach ($a in $Argv) {
        if ($a -ne '' -and $a -notmatch '[\s"]') {
            $a
        } else {
            $escaped = [regex]::Replace($a, '(\\*)"', '$1$1\"')
            $escaped = [regex]::Replace($escaped, '(\\+)$', '$1$1')
            '"' + $escaped + '"'
        }
    }
    return ($out -join ' ')
}

# Run one command and return its wall-clock duration in milliseconds.
#
# The stopwatch brackets the whole child process: spawn, run, exit. That is what
# a developer waits for, so that is what is measured. Output is drained into
# strings rather than piped into PowerShell, whose object pipeline would add its
# own marshalling cost to the measurement.
function Measure-Command-Ms {
    param(
        [Parameter(Mandatory)][string]   $Exe,
        [Parameter(Mandatory)][string[]] $Argv,
        [string] $WorkDir = $null,
        [switch] $AllowFailure
    )

    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName               = $Exe
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError  = $true
    $psi.UseShellExecute        = $false
    if ($WorkDir) { $psi.WorkingDirectory = $WorkDir }
    # Windows PowerShell 5.1's ProcessStartInfo has no ArgumentList, only the
    # single Arguments string, so the argv is quoted here by the same rule
    # CommandLineToArgvW parses back.
    $psi.Arguments = ConvertTo-WindowsArgLine $Argv

    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $p  = [System.Diagnostics.Process]::Start($psi)
    # Drain both pipes before waiting. A child that fills a redirected pipe
    # blocks on write, and the wait would then never return.
    $stdout = $p.StandardOutput.ReadToEndAsync()
    $stderr = $p.StandardError.ReadToEndAsync()
    $p.WaitForExit()
    $sw.Stop()
    $null = $stdout.Result
    $err  = $stderr.Result

    if ($p.ExitCode -ne 0 -and -not $AllowFailure) {
        throw "$Exe $($Argv -join ' ') exited $($p.ExitCode): $err"
    }
    return $sw.Elapsed.TotalMilliseconds
}

# Nearest-rank percentile on a sorted sample.
#
# Nearest-rank rather than an interpolating definition because these are whole
# observed runs: every value this returns is a run that actually happened. Note
# that p99 of n samples is the maximum whenever n is under 100, which is why
# BENCHMARKS.md prints the sample count beside every percentile.
function Get-Percentile {
    param([double[]] $Values, [double] $P)
    $sorted = $Values | Sort-Object
    $rank   = [math]::Ceiling($P * $sorted.Count)
    if ($rank -lt 1) { $rank = 1 }
    return $sorted[$rank - 1]
}

function New-Summary {
    param([string] $Case, [double[]] $Samples)
    return [pscustomobject]@{
        case   = $Case
        n      = $Samples.Count
        min_ms = [math]::Round(($Samples | Measure-Object -Minimum).Minimum, 1)
        p50_ms = [math]::Round((Get-Percentile $Samples 0.50), 1)
        p90_ms = [math]::Round((Get-Percentile $Samples 0.90), 1)
        p99_ms = [math]::Round((Get-Percentile $Samples 0.99), 1)
        max_ms = [math]::Round(($Samples | Measure-Object -Maximum).Maximum, 1)
    }
}

# Write raw per-run samples so a summary can be recomputed without re-running.
function Save-Samples {
    param([string] $Name, [hashtable] $Cases)
    $rows = foreach ($case in $Cases.Keys | Sort-Object) {
        $i = 0
        foreach ($ms in $Cases[$case]) {
            $i++
            [pscustomobject]@{ case = $case; run = $i; ms = [math]::Round($ms, 1) }
        }
    }
    $path = Join-Path $script:OutDir "$Name-samples.csv"
    $rows | Export-Csv -Path $path -NoTypeInformation -Encoding utf8
    Write-Host "raw samples: $path"
}

function Save-Summary {
    param([string] $Name, [object[]] $Summaries)
    $path = Join-Path $script:OutDir "$Name-summary.csv"
    $Summaries | Export-Csv -Path $path -NoTypeInformation -Encoding utf8
    Write-Host "summary: $path"
    $Summaries | Format-Table -AutoSize | Out-String | Write-Host
}

# A throwaway project directory holding one capsule.toml. The benchmarks must
# not measure capsule's upward search for a config through a deep tree, so the
# file sits directly in the directory capsule is run from.
function New-BenchProject {
    param([Parameter(Mandatory)][string] $Image, [string] $Name = 'bench')
    $dir = Join-Path ([System.IO.Path]::GetTempPath()) ("capsule-bench-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Path $dir | Out-Null
    $toml = @"
capsule = ">=0.2"
name    = "$Name"
image   = "$Image"
shell   = "/bin/sh"
workdir = "/workspace"
"@
    # Written without a byte-order mark. `Set-Content -Encoding utf8` on Windows
    # PowerShell 5.1 writes one, and the fixture must be a plain capsule.toml
    # rather than a test of how the reader handles a BOM.
    [System.IO.File]::WriteAllText(
        (Join-Path $dir 'capsule.toml'), $toml,
        (New-Object System.Text.UTF8Encoding($false)))
    return $dir
}

# Render one argv element the way capsule's own Display does, so a
# reconstructed command line can be compared against what `capsule up --dry-run`
# prints. This mirrors runtime.needsQuoting and runtime.shellQuote; the point of
# duplicating it is that the comparison fails loudly if either one changes.
function Format-ShellWord {
    param([string] $Word)
    $special = " `t`n'`"\`$``&|;<>()*?[]#~!"
    $needs = ($Word -eq '')
    if (-not $needs) {
        foreach ($ch in $Word.ToCharArray()) {
            if ($special.IndexOf($ch) -ge 0) { $needs = $true; break }
        }
    }
    if (-not $needs) { return $Word }
    return "'" + $Word.Replace("'", "'\''") + "'"
}

# Run docker for its exit code, capturing both pipes.
#
# Not `& docker ... 2>&1`: Windows PowerShell 5.1 turns a native command's
# redirected stderr into error records and reports failure even on exit 0, which
# would make a benchmark abort on a docker message that was only informational.
function Invoke-Docker {
    param([Parameter(Mandatory)][string[]] $Argv)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName               = 'docker'
    $psi.Arguments              = ConvertTo-WindowsArgLine $Argv
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError  = $true
    $psi.UseShellExecute        = $false
    $p = [System.Diagnostics.Process]::Start($psi)
    $out = $p.StandardOutput.ReadToEndAsync()
    $err = $p.StandardError.ReadToEndAsync()
    $p.WaitForExit()
    return [pscustomobject]@{ code = $p.ExitCode; out = $out.Result; err = $err.Result }
}

function Remove-Image {
    param([string] $Image)
    $null = Invoke-Docker @('rmi', '-f', $Image)
}

function Test-ImagePresent {
    param([string] $Image)
    return ((Invoke-Docker @('image', 'inspect', $Image)).code -eq 0)
}

