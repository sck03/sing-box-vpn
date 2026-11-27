[CmdletBinding(SupportsShouldProcess)]
param(
    [switch]$Dependencies,
    [switch]$BrowserTools,
    [switch]$Tools
)

$ErrorActionPreference = 'Stop'
$cleanupRoot = (Resolve-Path -LiteralPath (Split-Path -Parent $PSScriptRoot)).Path
if (-not (Test-Path -LiteralPath (Join-Path $cleanupRoot 'go.mod')) -or
    -not (Test-Path -LiteralPath (Join-Path $cleanupRoot 'package.json'))) {
    throw 'Run the cleanup script from a project checkout.'
}
$cleanupPrefix = $cleanupRoot.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
$cleanupNames = @('.test', '.build', '.cleanup-manual')
if ($Dependencies) { $cleanupNames += 'node_modules' }
if ($Tools) { $cleanupNames += '.tools' }
elseif ($BrowserTools) { $cleanupNames += '.tools/python313-packages' }

# Validate every target before deleting anything. Never follow directory links.
$cleanupSkipped = [Collections.Generic.List[string]]::new()
$cleanupTargets = foreach ($name in $cleanupNames) {
    $candidate = Join-Path $cleanupRoot $name
    if (-not (Test-Path -LiteralPath $candidate)) { continue }
    $resolved = (Resolve-Path -LiteralPath $candidate).Path
    if (-not $resolved.StartsWith($cleanupPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Cleanup target leaves the project: $resolved"
    }
    $item = Get-Item -LiteralPath $resolved -Force
    for ($ancestor = $item; $ancestor.FullName -ne $cleanupRoot; $ancestor = $ancestor.Parent) {
        if ($ancestor.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw "Refusing linked cleanup target: $($ancestor.FullName)"
        }
    }
    try {
        $children = @(Get-ChildItem -LiteralPath $resolved -Force -Recurse -ErrorAction Stop)
    } catch [UnauthorizedAccessException] {
        # An inaccessible old tree must not block cleanup of independent caches.
        $cleanupSkipped.Add($resolved)
        continue
    }
    if ($children.Where({ $_.Attributes -band [IO.FileAttributes]::ReparsePoint }).Count) {
        throw "Refusing a cleanup tree containing links: $resolved"
    }
    $running = @(Get-Process | Where-Object { $_.Path -and $_.Path.StartsWith($resolved + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) })
    if ($running.Count) { throw "Stop running executables in $resolved before cleanup." }
    [pscustomobject]@{
        Path = $resolved
        Bytes = ($children | Where-Object { -not $_.PSIsContainer } | Measure-Object -Property Length -Sum).Sum
    }
}

$cleanupBytes = 0L
foreach ($target in $cleanupTargets) {
    if ($PSCmdlet.ShouldProcess($target.Path, 'Remove rebuildable development artifacts')) {
        # Explicit files first: module caches can contain read-only files, and
        # non-recursive removal keeps each deletion inside the validated tree.
        Get-ChildItem -LiteralPath $target.Path -Force -Recurse -File |
            ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force }
        Get-ChildItem -LiteralPath $target.Path -Force -Recurse -Directory |
            Sort-Object { $_.FullName.Length } -Descending |
            ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force }
        Remove-Item -LiteralPath $target.Path -Force
        $cleanupBytes += $target.Bytes
        Write-Output ('Removed {0}: {1:N2} MiB' -f $target.Path, ($target.Bytes / 1MB))
    }
}
Write-Output ('Freed rebuildable artifacts: {0:N2} MiB' -f ($cleanupBytes / 1MB))
foreach ($path in $cleanupSkipped) {
    Write-Warning "Skipped inaccessible directory: $path. Its Windows permissions require separate repair."
}
