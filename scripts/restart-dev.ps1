# Rebuilds and restarts the development master server from a data directory.
# Usage: pwsh scripts/restart-dev.ps1 [-Port 30002] [-Data .data] [-NoBuild]
param(
    [int]$Port = 30002,
    [string]$Data = ".data",
    [switch]$NoBuild
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $pidFile = Join-Path $root ".dist\server-$Port.pid"
    $logFile = Join-Path $root ".dist\server-$Port.log"
    $errFile = Join-Path $root ".dist\server-$Port.err.log"

    if (Test-Path $pidFile) {
        $old = Get-Content $pidFile -ErrorAction SilentlyContinue
        if ($old) {
            Stop-Process -Id $old -ErrorAction SilentlyContinue
        }
    }
    Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 500

    if (-not $NoBuild) {
        $env:GOTOOLCHAIN = "local"
        go build -o ".dist\cso-server.exe" ".\cmd\cso-server"
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    }

    New-Item -ItemType Directory -Force -Path (Join-Path $root ".dist") | Out-Null
    $process = Start-Process -FilePath (Join-Path $root ".dist\cso-server.exe") `
        -ArgumentList @("-data", $Data, "-debug") `
        -RedirectStandardOutput $logFile `
        -RedirectStandardError $errFile `
        -PassThru
    $process.Id | Set-Content $pidFile
    Start-Sleep -Seconds 2

    if ($process.HasExited) {
        Write-Output "server exited with code $($process.ExitCode)"
        Get-Content $errFile -Tail 20
        exit 1
    }
    $tcp = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    $udp = Get-NetUDPEndpoint -LocalPort $Port -ErrorAction SilentlyContinue
    Write-Output ("pid={0} tcp={1} udp={2} data={3}" -f $process.Id, [bool]$tcp, [bool]$udp, $Data)
    Get-Content $errFile -Tail 5
}
finally {
    Pop-Location
}
