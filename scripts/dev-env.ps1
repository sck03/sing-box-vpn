# Dot-source from PowerShell: . ./scripts/dev-env.ps1
$projectRoot = Split-Path -Parent $PSScriptRoot
$projectTools = Join-Path $projectRoot '.tools'
$projectTest = Join-Path $projectRoot '.test'
$projectVersions = Get-Content -LiteralPath (Join-Path $projectRoot 'versions.json') -Raw | ConvertFrom-Json
$projectGo = Join-Path $projectTools 'go/bin/go.exe'
if (-not (Test-Path -LiteralPath $projectGo)) {
    $installedGo = Get-Command go -CommandType Application -ErrorAction SilentlyContinue
    if (-not $installedGo) { throw "Install Go $($projectVersions.go) on PATH or under .tools/go, then run this script again." }
    $projectGo = $installedGo.Source
}
$projectGoVersion = & $projectGo version
if ($LASTEXITCODE -ne 0 -or $projectGoVersion -notmatch ('\bgo' + [regex]::Escape($projectVersions.go) + '\b')) {
    throw "Expected Go $($projectVersions.go); found: $projectGoVersion"
}
foreach ($name in @('gocache', 'gomodcache', 'gopath', 'tmp')) {
    New-Item -ItemType Directory -Force -Path (Join-Path $projectTest $name) | Out-Null
}
$env:GOCACHE = Join-Path $projectTest 'gocache'
$env:GOMODCACHE = Join-Path $projectTest 'gomodcache'
$env:GOPATH = Join-Path $projectTest 'gopath'
$env:GOTMPDIR = Join-Path $projectTest 'tmp'
$env:TEMP = $env:GOTMPDIR
$env:TMP = $env:GOTMPDIR
$env:GOTOOLCHAIN = 'local'
$env:GO_BINARY = $projectGo
$env:PATH = (Split-Path -Parent $projectGo) + [IO.Path]::PathSeparator + $env:PATH
$projectCore = Join-Path $projectTools ('sing-box-' + $projectVersions.sing_box.tested_version + '-windows-amd64/sing-box.exe')
if (Test-Path -LiteralPath $projectCore) {
    $env:SING_BOX_BINARY = $projectCore
}
$clientTestVersions = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'client-test-versions.json') -Raw | ConvertFrom-Json
$projectMihomo = Join-Path $projectTools ('mihomo-' + $clientTestVersions.mihomo.version + '/mihomo-windows-amd64.exe')
if (Test-Path -LiteralPath $projectMihomo) {
    $env:MIHOMO_BINARY = $projectMihomo
}
