param(
    [ValidateRange(1, 1000)]
    [int]$Trials = 25,
    [string]$OutDir = ('.local-run\agentpay-benchmark-' + (Get-Date -Format 'yyyyMMddTHHmmss'))
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $repo 'commerce-runtime'
$dataset = Join-Path $runtimeDir 'testdata\s11\agentpay-benchmark\scenarios.jsonl'
$absoluteOutDir = [IO.Path]::GetFullPath((Join-Path $repo $OutDir))
$repoPrefix = [IO.Path]::GetFullPath($repo).TrimEnd('\') + '\'
if (-not $absoluteOutDir.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'OutDir must resolve inside the StablePay repository.'
}
if (Test-Path -LiteralPath $absoluteOutDir) {
    if (Get-ChildItem -LiteralPath $absoluteOutDir -Force | Select-Object -First 1) {
        throw "OutDir is not empty; choose a new output directory: $absoluteOutDir"
    }
} else {
    New-Item -ItemType Directory -Force -Path $absoluteOutDir | Out-Null
}

$binDir = Join-Path $repo '.local-run\bin-agentpay-benchmark'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$suiteExe = Join-Path $binDir 's11-live-local-suite.exe'
$cliExe = Join-Path $binDir 'stablepay-runtime.exe'
$env:GOCACHE = Join-Path $repo '.local-run\gocache-agentpay-benchmark'
$env:GOMODCACHE = Join-Path $repo '.local-run\gomodcache-l2'
$env:GOSUMDB = 'off'

Push-Location $runtimeDir
try {
    go build -o $suiteExe ./cmd/s11-live-local-suite
    if ($LASTEXITCODE -ne 0) { throw 'failed to build s11-live-local-suite' }
    go build -o $cliExe ./cmd/stablepay-runtime
    if ($LASTEXITCODE -ne 0) { throw 'failed to build stablepay-runtime CLI' }
    & $suiteExe --dataset $dataset --out-dir $absoluteOutDir --runtime-cli $cliExe --trials $Trials
    if ($LASTEXITCODE -ne 0) { throw 'AgentPay live-local benchmark failed' }
    Write-Host "AgentPay benchmark artifacts: $absoluteOutDir"
} finally {
    Pop-Location
}
