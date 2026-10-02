# Installs the latest otmux on Windows, for the current user (no admin):
#
#   irm https://raw.githubusercontent.com/vamsikartik01/osmite-RnD/main/otmux/install.ps1 | iex
#
# It downloads otmux.exe from the rolling otmux/latest release, checks it
# against the published SHA-256 checksums, puts it in
# %LOCALAPPDATA%\Programs\otmux, and adds that folder to your PATH.
#
# For testing: -From <folder> installs from a local folder holding
# otmux-windows-*.exe and checksums.txt (e.g. dist/ from release.ps1), and
# -NoPath leaves PATH alone.

param(
    [string]$From = "",
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\otmux"),
    [switch]$NoPath
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue" # much faster Invoke-WebRequest
$base = "https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest"

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$asset = "otmux-windows-$arch.exe"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("otmux-install-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Force $tmp | Out-Null

try {
    Write-Host "Downloading otmux ($arch)..."
    if ($From) {
        Copy-Item (Join-Path $From $asset) (Join-Path $tmp $asset)
        Copy-Item (Join-Path $From "checksums.txt") (Join-Path $tmp "checksums.txt")
    } else {
        Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
        Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt") -UseBasicParsing
    }

    # Verify the download.
    $line = Get-Content (Join-Path $tmp "checksums.txt") | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))$" }
    if (-not $line) { throw "checksums.txt has no entry for $asset" }
    $want = ($line -split "\s+")[0].ToLower()
    $got = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
    if ($want -ne $got) { throw "checksum mismatch for $asset (expected $want, got $got)" }

    # Install. A running otmux keeps its exe locked, but Windows lets us move
    # it aside, so the new one can take its place; the old one is cleaned up
    # on the next install.
    New-Item -ItemType Directory -Force $InstallDir | Out-Null
    $exe = Join-Path $InstallDir "otmux.exe"
    Get-ChildItem $InstallDir -Filter "otmux.old-*.exe" -ErrorAction SilentlyContinue |
        Remove-Item -Force -ErrorAction SilentlyContinue
    if (Test-Path $exe) {
        Move-Item $exe (Join-Path $InstallDir ("otmux.old-" + [Guid]::NewGuid() + ".exe"))
    }
    Move-Item (Join-Path $tmp $asset) $exe

    # Put it on the user's PATH.
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @($userPath -split ";" | Where-Object { $_ })
    if (-not $NoPath -and $parts -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable("Path", (($parts + $InstallDir) -join ";"), "User")
        $env:Path = "$env:Path;$InstallDir"
        $pathNote = " Open a new terminal to use 'otmux' from anywhere."
    } else {
        $pathNote = ""
    }

    $version = & $exe version
    Write-Host "Installed $version to $exe.$pathNote"
    Write-Host "If otmux was already running, restart it to use the new version: otmux kill-server"
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
