# Builds otmux release files locally, exactly as the release workflow does:
# for every platform, an archive and a bare binary with a fixed name, plus
# checksums.txt, all in dist/.
#
#   ./release.ps1                 # version from internal/version, every platform
#   ./release.ps1 -Version 1.0.1
#   ./release.ps1 -Os windows     # only some platforms (comma-separated)
#
# Official releases are built by .github/workflows/otmux-release.yml when a
# tag like otmux/v1.0.0 is pushed; use this to try a release build first, or
# as a fallback.

param([string]$Version = "", [string]$Os = "windows,linux,darwin")

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not $Version) {
    $m = Select-String -Path "internal/version/version.go" -Pattern 'Version = "([^"]+)"'
    $Version = $m.Matches[0].Groups[1].Value
}
Write-Host "Building otmux $Version"

go test -count=1 -short ./...
if ($LASTEXITCODE -ne 0) { throw "tests failed" }

Remove-Item -Recurse -Force dist, build -ErrorAction SilentlyContinue
New-Item -ItemType Directory dist, build | Out-Null

$wanted = $Os -split "," | ForEach-Object { $_.Trim() }
$targets = "windows/amd64", "windows/arm64", "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64" |
    Where-Object { $wanted -contains ($_ -split "/")[0] }
$env:CGO_ENABLED = "0"
foreach ($t in $targets) {
    $os, $arch = $t -split "/"
    $ext = if ($os -eq "windows") { ".exe" } else { "" }
    $name = "otmux_${Version}_${os}_${arch}"
    New-Item -ItemType Directory "build/$name" | Out-Null

    $env:GOOS = $os; $env:GOARCH = $arch
    go build -trimpath -ldflags "-s -w -X github.com/vamsikartik01/osmite-RnD/otmux/internal/version.Version=$Version -X github.com/vamsikartik01/osmite-RnD/otmux/internal/version.Release=true" `
        -o "build/$name/otmux$ext" ./cmd/otmux
    if ($LASTEXITCODE -ne 0) { throw "build failed for $t" }
    Copy-Item README.md, ../LICENSE "build/$name/"

    Copy-Item "build/$name/otmux$ext" "dist/otmux-$os-$arch$ext"
    if ($os -eq "windows") {
        Compress-Archive "build/$name" "dist/$name.zip"
    } else {
        tar -C build -czf "dist/$name.tar.gz" $name
        if ($LASTEXITCODE -ne 0) { throw "tar failed for $t" }
    }
    Write-Host "  $t"
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

# version.txt tells running copies of otmux which version is newest.
[IO.File]::WriteAllText((Join-Path $PSScriptRoot "dist/version.txt"), $Version)
if (-not (Test-Path dist/version.txt)) { throw "version.txt wasn't written" }

# Same format as sha256sum, so install.ps1 can check either.
Get-ChildItem dist | Where-Object Name -ne "checksums.txt" | Sort-Object Name | ForEach-Object {
    "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
} | Set-Content -Encoding ascii dist/checksums.txt

Remove-Item -Recurse -Force build
Write-Host "Done:"
Get-ChildItem dist | ForEach-Object { "  {0,-34} {1,10:N0} bytes" -f $_.Name, $_.Length }
