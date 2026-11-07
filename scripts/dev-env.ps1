# Dot-source from PowerShell: . ./scripts/dev-env.ps1
$projectRoot = Split-Path -Parent $PSScriptRoot
$projectTools = Join-Path $projectRoot '.tools'
$projectTest = Join-Path $projectRoot '.test'
foreach ($name in @('gocache', 'gomodcache', 'tmp')) {
    New-Item -ItemType Directory -Force -Path (Join-Path $projectTest $name) | Out-Null
}
$env:GOCACHE = Join-Path $projectTest 'gocache'
$env:GOMODCACHE = Join-Path $projectTest 'gomodcache'
$env:GOPATH = Join-Path $projectTools 'gopath'
$env:GOTMPDIR = Join-Path $projectTest 'tmp'
$env:TEMP = $env:GOTMPDIR
$env:TMP = $env:GOTMPDIR
$env:GOTOOLCHAIN = 'local'
$env:GO_BINARY = Join-Path $projectTools 'go/bin/go.exe'
$env:PATH = (Join-Path $projectTools 'go/bin') + [IO.Path]::PathSeparator + $env:PATH
$projectCore = Join-Path $projectTools 'sing-box-1.14.4-windows-amd64/sing-box.exe'
if (Test-Path -LiteralPath $projectCore) {
    $env:SING_BOX_BINARY = $projectCore
}
$projectMihomo = Join-Path $projectTools 'mihomo-1.19.33/mihomo-windows-amd64.exe'
if (Test-Path -LiteralPath $projectMihomo) {
    $env:MIHOMO_BINARY = $projectMihomo
}
