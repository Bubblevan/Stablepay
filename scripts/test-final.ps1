[CmdletBinding()]
param(
    [switch]$SkipSixServices,
    [switch]$IncludeMySQL,
    [switch]$IncludeS11LiveLocal
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$runtime = Join-Path $root 'commerce-runtime'
$runDir = Join-Path $root '.local-run'
New-Item -ItemType Directory -Force -Path $runDir | Out-Null

# Keep compiler/test cache writes inside the repository's local run area.
$env:GOCACHE = Join-Path $runDir 'gocache-final-runtime'

function Invoke-Gate([string]$Label, [string]$WorkingDirectory, [scriptblock]$Command) {
    Write-Host "[final] $Label"
    Push-Location $WorkingDirectory
    try {
        & $Command
        if ($LASTEXITCODE -ne 0) { throw "$Label failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

if (-not $SkipSixServices) {
    Invoke-Gate 'six-service and canonical IDL gate' $root {
        & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root 'scripts\test-six-services.ps1')
    }
}

Invoke-Gate 'commerce-runtime go test count=1' $runtime { go test ./... -count=1 }
Invoke-Gate 'commerce-runtime go test count=3' $runtime { go test ./... -count=3 }
Invoke-Gate 'commerce-runtime go vet' $runtime { go vet ./... }
Invoke-Gate 'git diff check' $root { git diff --check }

if ($IncludeMySQL) {
    Invoke-Gate 'commerce-runtime MySQL integration' $runtime { go test ./internal/infrastructure/mysql -count=1 -v }
}

if ($IncludeS11LiveLocal) {
    Invoke-Gate 'S11 live-local external-client suite' $root {
        & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root 'scripts\test-s11-live-local.ps1')
    }
}

Write-Host '[final] deterministic regression gate passed'
Write-Host '[final] paid LLM and real Devnet smoke were not invoked by this script'
