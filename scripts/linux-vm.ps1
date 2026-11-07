param([ValidateSet('start', 'stop', 'status')][string]$Action = 'status')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$projectTools = Join-Path $projectRoot '.tools'
$projectTest = Join-Path $projectRoot '.test'
$qemu = Join-Path $projectTools 'qemu/qemu-system-x86_64.exe'
$disk = Join-Path $projectTools 'linux-validation.qcow2'
$seed = Join-Path $projectTools 'linux-seed.iso'
$pidFile = Join-Path $projectTest 'linux-vm.pid'
$vm = $null
if (Test-Path -LiteralPath $pidFile) {
    $vmID = [int]([IO.File]::ReadAllText($pidFile).Trim())
    $candidate = Get-Process -Id $vmID -ErrorAction SilentlyContinue
    if ($candidate -and $candidate.Path -eq $qemu) { $vm = $candidate }
}
if ($Action -eq 'status') {
    if ($vm) { "Linux VM running: PID $($vm.Id), SSH 127.0.0.1:22222" } else { 'Linux VM stopped' }
    return
}
if ($Action -eq 'stop') {
    if (!$vm) { 'Linux VM already stopped'; return }
    & ssh.exe -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new `
        -o "UserKnownHostsFile=$projectTools/linux-known-hosts" -i "$projectTools/linux-key" `
        -p 22222 validator@127.0.0.1 'sudo shutdown -h now'
    if ($LASTEXITCODE -ne 0) { throw 'SSH shutdown failed; inspect .test/linux-console.log before stopping the VM manually.' }
    'Linux shutdown requested; wait for status to report stopped before moving its disk.'
    return
}
if ($vm) { "Linux VM already running: PID $($vm.Id)"; return }
foreach ($file in @($qemu, $disk, $seed)) {
    if (!(Test-Path -LiteralPath $file)) { throw "Missing local validation asset: $file" }
}
New-Item -ItemType Directory -Force -Path $projectTest | Out-Null
$vm = Start-Process -FilePath $qemu -ArgumentList @(
    '-accel', 'tcg,thread=multi', '-m', '2048', '-smp', '2',
    '-drive', "`"file=$disk,if=virtio`"",
    '-drive', "`"file=$seed,format=raw,media=cdrom,readonly=on`"",
    '-netdev', 'user,id=net0,hostfwd=tcp:127.0.0.1:22222-:22',
    '-device', 'virtio-net-pci,netdev=net0', '-display', 'none',
    '-serial', "`"file:$projectTest/linux-console.log`"", '-monitor', 'none'
) -WindowStyle Hidden -PassThru -RedirectStandardError "$projectTest/qemu-error.log"
if ($vm.WaitForExit(1000)) {
    throw "Linux VM exited during startup (code $($vm.ExitCode)); inspect .test/qemu-error.log."
}
[IO.File]::WriteAllText($pidFile, [string]$vm.Id)
"Linux VM started: PID $($vm.Id), SSH 127.0.0.1:22222"
