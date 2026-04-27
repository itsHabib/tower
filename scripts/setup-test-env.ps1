# PowerShell version of scripts/setup-test-env.sh.
#
# Spin up an isolated tower sandbox for manual TUI testing on Windows.
# - Builds tower.exe (idempotent)
# - Builds and runs scripts/seed which creates ~6 fake repos with a
#   handful of worktrees each, in varied state (clean, dirty, ahead).
# - Points APPDATA at a sandbox dir so tower's state.db is isolated
#   from your real tower state.
# - Prints the exact command to launch the TUI.
#
# Re-running the script wipes the sandbox and starts over.
#
# Usage (from anywhere):  pwsh scripts/setup-test-env.ps1
#         (or)            powershell -File scripts\setup-test-env.ps1

$ErrorActionPreference = 'Stop'

$RootDir  = Resolve-Path (Join-Path $PSScriptRoot '..')
$Sandbox  = Join-Path $RootDir '.sandbox'
$ReposDir = Join-Path $Sandbox 'repos'
$State    = Join-Path $Sandbox 'state'
$Bin      = Join-Path $RootDir 'tower.exe'

Write-Host "==> sandbox at $Sandbox"
if (Test-Path $Sandbox) {
    Remove-Item -Recurse -Force $Sandbox
}
New-Item -ItemType Directory -Path $ReposDir -Force | Out-Null
New-Item -ItemType Directory -Path $State    -Force | Out-Null

Write-Host "==> building tower.exe"
Push-Location $RootDir
try {
    & go build -o tower.exe ./cmd/tower
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally { Pop-Location }

Write-Host "==> seeding playground"
Push-Location $RootDir
try {
    & go run ./scripts/seed -root $ReposDir -state $State
    if ($LASTEXITCODE -ne 0) { throw "seed failed" }
} finally { Pop-Location }

Write-Host ""
Write-Host "==> sandbox ready." -ForegroundColor Green
Write-Host ""
Write-Host "    sandbox APPDATA: $State"
Write-Host "    fake repos:      $ReposDir"
Write-Host ""
Write-Host "Launch the TUI with:"
Write-Host ""
Write-Host "    `$env:APPDATA='$State'; & '$Bin'" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Add `$env:TOWER_DEBUG=1 before launching to write trace logs to %TEMP%\tower-debug.log."
Write-Host ""
Write-Host "Press q to quit the TUI; the sandbox files stay until you re-run this script."
