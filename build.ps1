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

.PARAMETER Docker
    Build the Linux binary inside a golang Docker container instead of using the host
    Go toolchain. Produces dist/proby_linux_<arch>. Useful for a clean/reproducible
    Linux build. Combine with -All to build both linux/amd64 and linux/arm64.

.PARAMETER Arch
    Target architecture for -Docker (amd64 or arm64). Default amd64.

.PARAMETER Image
    Docker image used for -Docker builds. Default golang:1.26-alpine.

.EXAMPLE
    .\build.ps1
.EXAMPLE
    .\build.ps1 -Version v0.2.0
.EXAMPLE
    .\build.ps1 -All
.EXAMPLE
    .\build.ps1 -Docker                 # linux/amd64 via container
.EXAMPLE
    .\build.ps1 -Docker -Arch arm64
.EXAMPLE
    .\build.ps1 -Docker -All            # linux amd64 + arm64 via container
#>
[CmdletBinding()]
param(
    [string]$Version,
    [string]$Output,
    [switch]$All,
    [switch]$Docker,
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",
    [string]$Image = "golang:1.26-alpine"
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

# Invoke-DockerBuild builds a Linux binary inside a golang container. The repo is
# mounted read-write; module/build caches persist in named volumes for fast rebuilds.
# Version metadata is computed on the host and passed in via -ldflags.
function Invoke-DockerBuild([string]$TargetArch) {
    New-Item -ItemType Directory -Force -Path "dist" | Out-Null
    $out = "dist/proby_linux_$TargetArch"
    Write-Host "  building linux/$TargetArch in $Image (docker) -> $out"
    # Docker Desktop accepts native Windows paths for -v.
    & docker run --rm `
        -v "$($PSScriptRoot):/src" `
        -v "proby-go-mod:/go/pkg/mod" `
        -v "proby-go-build:/root/.cache/go-build" `
        -w /src `
        -e CGO_ENABLED=0 -e GOOS=linux -e "GOARCH=$TargetArch" `
        $Image `
        go build -trimpath -ldflags "$LdFlags" -o $out ./cmd/proby
    if ($LASTEXITCODE -ne 0) { throw "docker build failed for linux/$TargetArch" }
}

function Assert-Docker {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "docker not found on PATH. Install Docker Desktop, or drop -Docker to cross-compile with the host Go toolchain."
    }
}

try {
    if ($Docker) {
        Assert-Docker
        if ($All) {
            Invoke-DockerBuild "amd64"
            Invoke-DockerBuild "arm64"
        }
        else {
            Invoke-DockerBuild $Arch
        }
        Write-Host ""
        Write-Host "Done. Linux artifacts in dist/:" -ForegroundColor Green
        Get-ChildItem dist -Filter "proby_linux_*" | Format-Table Name, @{n = "Size(MB)"; e = { [math]::Round($_.Length / 1MB, 1) } } -AutoSize
    }
    elseif ($All) {
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
