$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$Source = Join-Path $Root "dist"
$Target = Join-Path $Root "backend\web\dist"

if (-not (Test-Path -LiteralPath $Source)) {
    throw "Frontend build output not found: $Source"
}

New-Item -ItemType Directory -Force -Path $Target | Out-Null
Get-ChildItem -LiteralPath $Source -Force | Copy-Item -Destination $Target -Recurse -Force

Write-Host "Synced frontend dist to $Target"
