<#
.SYNOPSIS
    Build the proby binary with version information injected via -ldflags.

.DESCRIPTION
    Stamps Version / Commit / Date into internal/version at build time. By default it
    builds for the host platform; use -All to cross-compile the full release matrix.

.PARAMETER Version
    Version string to embed. Defaults to `git describe` (tag or short commit), else "dev".

.PARAMETER Output
    Output path for the host build. Defaults to proby.exe (Windows) / proby.

.PARAMETER All
    Cross-compile every supported OS/arch into dist/ instead of a single host binary.

.EXAMPLE
    .\build.ps1
.EXAMPLE
    .\build.ps1 -Version v0.2.0
.EXAMPLE
    .\build.ps1 -All
#>
[CmdletBinding()]
param(
    [string]$Version,
    [string]$Output,
    [switch]$All
)

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

$VersionPkg = "github.com/stansat/proby/internal/version"

# --- Derive build metadata -------------------------------------------------
function Get-GitOutput([string[]]$GitArgs) {
    try {
        $out = & git @GitArgs 2>$null
        if ($LASTEXITCODE -eq 0 -and $out) { return ($out | Select-Object -First 1).ToString().Trim() }
    } catch {}
    return $null
}

if (-not $Version) {
    $Version = Get-GitOutput @("describe", "--tags", "--always", "--dirty")
    if (-not $Version) { $Version = "dev" }
}
$Commit = Get-GitOutput @("rev-parse", "--short", "HEAD")
if (-not $Commit) { $Commit = "none" }
$BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$LdFlags = "-s -w " +
    "-X $VersionPkg.Version=$Version " +
    "-X $VersionPkg.Commit=$Commit " +
    "-X $VersionPkg.Date=$BuildDate"

Write-Host "proby build" -ForegroundColor Cyan
Write-Host "  version : $Version"
Write-Host "  commit  : $Commit"
Write-Host "  date    : $BuildDate"
Write-Host ""

# --- Save and later restore environment ------------------------------------
$savedCgo  = $env:CGO_ENABLED
$savedGoos = $env:GOOS
$savedArch = $env:GOARCH
$env:CGO_ENABLED = "0"

function Restore-Env {
    $env:CGO_ENABLED = $savedCgo
    $env:GOOS        = $savedGoos
    $env:GOARCH      = $savedArch
}

function Invoke-Build([string]$OutFile) {
    & go build -trimpath -ldflags "$LdFlags" -o $OutFile ./cmd/proby
    if ($LASTEXITCODE -ne 0) { throw "go build failed for $OutFile" }
}

try {
    if ($All) {
        $matrix = @(
            @{ os = "linux";   arch = "amd64"; ext = "" },
            @{ os = "linux";   arch = "arm64"; ext = "" },
            @{ os = "windows"; arch = "amd64"; ext = ".exe" },
            @{ os = "windows"; arch = "arm64"; ext = ".exe" },
            @{ os = "darwin";  arch = "amd64"; ext = "" },
            @{ os = "darwin";  arch = "arm64"; ext = "" }
        )
        New-Item -ItemType Directory -Force -Path "dist" | Out-Null
        foreach ($t in $matrix) {
            $env:GOOS   = $t.os
            $env:GOARCH = $t.arch
            $out = "dist/proby_$($t.os)_$($t.arch)$($t.ext)"
            Write-Host "  building $($t.os)/$($t.arch) -> $out"
            Invoke-Build $out
        }
        Write-Host ""
        Write-Host "Done. Artifacts in dist/:" -ForegroundColor Green
        Get-ChildItem dist | Format-Table Name, @{n="Size(MB)";e={[math]::Round($_.Length/1MB,1)}} -AutoSize
    }
    else {
        if (-not $Output) {
            # Default to proby.exe (host is Windows); append .exe only when not already there.
            if ($env:GOOS -and $env:GOOS -ne "windows") { $Output = "proby" } else { $Output = "proby.exe" }
        }
        Invoke-Build $Output
        Write-Host "Built $Output" -ForegroundColor Green
    }
}
finally {
    Restore-Env
}
