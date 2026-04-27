# PowerShell version of scripts/setup-test-env.sh.
#
# Spin up an isolated tower sandbox for manual TUI testing on Windows.
# - Builds tower.exe (idempotent)
# - Creates a fresh git repo with a few commits and a couple of
#   throwaway tower-style worktrees so the board has rows to play with.
# - Points APPDATA at a sandbox dir so tower's state.db is isolated
#   from your real tower state.
# - Prints the exact command to launch the TUI.
#
# Re-running the script wipes the sandbox and starts over.
#
# Usage (from anywhere):  pwsh scripts/setup-test-env.ps1
#         (or)            powershell -File scripts\setup-test-env.ps1

$ErrorActionPreference = 'Stop'

$RootDir = Resolve-Path (Join-Path $PSScriptRoot '..')
$Sandbox = Join-Path $RootDir '.sandbox'
$Repo    = Join-Path $Sandbox 'repo'
$State   = Join-Path $Sandbox 'state'
$Bin     = Join-Path $RootDir 'tower.exe'

Write-Host "==> sandbox at $Sandbox"
if (Test-Path $Sandbox) {
    Remove-Item -Recurse -Force $Sandbox
}
New-Item -ItemType Directory -Path $Repo  -Force | Out-Null
New-Item -ItemType Directory -Path $State -Force | Out-Null

Write-Host "==> building tower.exe"
Push-Location $RootDir
try {
    & go build -o tower.exe ./cmd/tower
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally { Pop-Location }

Write-Host "==> initializing throwaway git repo"
Push-Location $Repo
try {
    & git init -q
    & git config user.email sandbox@tower
    & git config user.name  sandbox
    Set-Content -Path 'README.md' -Value '# sandbox repo' -NoNewline
    & git add README.md
    & git commit -qm 'initial'
    Set-Content -Path 'a.txt' -Value 'feat 1' -NoNewline
    & git add a.txt
    & git commit -qm 'add a'
    Set-Content -Path 'b.txt' -Value 'feat 2' -NoNewline
    & git add b.txt
    & git commit -qm 'add b'
} finally { Pop-Location }

Write-Host "==> registering repo and seeding worktrees"
$env:APPDATA = $State
& $Bin repo add $Repo | Out-Null
& $Bin add --repo repo feat-x | Out-Null
& $Bin add --repo repo feat-y | Out-Null

# One dirty worktree + one with an unmerged commit, so every branch of
# the d-flow is exercisable by hand.
Set-Content -Path (Join-Path $Repo '.worktrees\feat-x\dirty.txt') -Value 'dirt' -NoNewline

Push-Location (Join-Path $Repo '.worktrees\feat-y')
try {
    & git config user.email sandbox@tower
    & git config user.name  sandbox
    Set-Content -Path 'only-on-y.txt' -Value 'secret' -NoNewline
    & git add only-on-y.txt
    & git commit -qm 'only on tower/feat-y (unmerged)'
} finally { Pop-Location }

Write-Host ""
Write-Host "==> sandbox ready." -ForegroundColor Green
Write-Host ""
Write-Host "    sandbox APPDATA: $State"
Write-Host "    sandbox repo:    $Repo"
Write-Host ""
Write-Host "Launch the TUI with:"
Write-Host ""
Write-Host "    `$env:APPDATA='$State'; & '$Bin'" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Add `$env:TOWER_DEBUG=1 before launching to write trace logs to %TEMP%\tower-debug.log."
Write-Host ""
Write-Host "Press q to quit the TUI; the sandbox files stay until you re-run this script."
