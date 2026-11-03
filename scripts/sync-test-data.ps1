# Mirrors the pristine data set into the writable test directory. The server is
# always started against the copy, so a restart starts from the same database and
# the source directory is never modified by a running instance.
# Usage: pwsh scripts/sync-test-data.ps1 [-Source .data] [-Target .data-test]
param(
    [string]$Source = ".data",
    [string]$Target = ".data-test"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    if (-not (Test-Path $Source)) { throw "source data directory $Source is missing" }

    # Exit codes 0..7 all mean success for robocopy (files copied, extra files
    # removed, nothing to do); 8 and above are failures.
    robocopy $Source $Target /MIR /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "robocopy failed with exit code $LASTEXITCODE" }

    $files = (Get-ChildItem $Target -Recurse -File | Measure-Object).Count
    Write-Output ("synced {0} -> {1}: {2} files" -f $Source, $Target, $files)
}
finally {
    Pop-Location
}
