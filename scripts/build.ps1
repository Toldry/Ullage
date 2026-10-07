# Builds the single Windows executable: the UI is compiled and embedded, then Go builds
# without cgo. Usage: scripts\build.ps1 [-Version 1.0.0]
param([string]$Version = "")
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
if (-not $Version) {
  $Version = (git describe --tags --always --dirty 2>$null)
  if (-not $Version) { $Version = "dev" }
}
Push-Location web
npm ci
if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
npm run build
if ($LASTEXITCODE -ne 0) { throw "npm run build failed" }
Pop-Location
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-H windowsgui -s -w -X main.version=$Version" `
  -o bin\ullage.exe .\cmd\ullage
if ($LASTEXITCODE -ne 0) { throw "go build failed" }
Write-Output "bin\ullage.exe ($Version)"
