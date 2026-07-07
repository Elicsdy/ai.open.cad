param(
  [ValidateSet("windows-amd64", "windows-arm64", "linux-amd64", "linux-arm64")]
  [string]$Target = "windows-amd64",
  [switch]$Snapshot
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Config = Join-Path $Root "goreleaser-$Target.yml"
$LockFile = Join-Path $Root ".cache\release.lock"

if (-not (Test-Path $Config)) {
  throw "GoReleaser config not found: $Config"
}

$env:GOCACHE = Join-Path $Root ".cache\go-build"
$env:GOMODCACHE = Join-Path $Root ".cache\gomod"
New-Item -ItemType Directory -Force -Path $env:GOCACHE, $env:GOMODCACHE | Out-Null

if (Test-Path $LockFile) {
  throw "Another release build appears to be running. Remove $LockFile if this is stale."
}
New-Item -ItemType File -Path $LockFile -Force | Out-Null

$args = @("release", "--clean", "--config", $Config)
if ($Snapshot) {
  $args += "--snapshot"
  $args += "--skip=validate"
}

Push-Location $Root
try {
  & goreleaser @args
  if ($LASTEXITCODE -ne 0) {
    throw "GoReleaser failed with exit code $LASTEXITCODE"
  }

  go run build/post/post-build.go
  if ($LASTEXITCODE -ne 0) {
    throw "post-build failed with exit code $LASTEXITCODE"
  }
} finally {
  Remove-Item -LiteralPath $LockFile -Force -ErrorAction SilentlyContinue
  Pop-Location
}
