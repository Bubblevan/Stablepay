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
}

$binDir = Join-Path $repo '.local-run\bin-agentpay-benchmark'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$suiteExe = Join-Path $binDir 's11-live-local-suite.exe'
$cliExe = Join-Path $binDir 'stablepay-runtime.exe'
$sourceStatus = @(& git -C $repo status --porcelain --untracked-files=all 2>$null)
if ($LASTEXITCODE -eq 0) {
    if ($sourceStatus.Count -gt 0) {
        $env:STABLEPAY_SOURCE_MODIFIED = 'true'
    } else {
        $env:STABLEPAY_SOURCE_MODIFIED = 'false'
    }
} else {
    Remove-Item Env:STABLEPAY_SOURCE_MODIFIED -ErrorAction SilentlyContinue
}
$env:GOCACHE = Join-Path $repo '.local-run\gocache-agentpay-benchmark'
$env:GOMODCACHE = Join-Path $repo '.local-run\gomodcache-l2'
$env:GOSUMDB = 'off'

Push-Location $runtimeDir
try {
    go build -o $suiteExe ./cmd/s11-live-local-suite
    if ($LASTEXITCODE -ne 0) { throw 'failed to build s11-live-local-suite' }
    go build -o $cliExe ./cmd/stablepay-runtime
    if ($LASTEXITCODE -ne 0) { throw 'failed to build stablepay-runtime CLI' }
    # Capture Go's VCS build metadata before creating a non-ignored output
    # directory; otherwise the evidence-producing run marks its own source as
    # modified merely because the destination exists during compilation.
    if (Test-Path -LiteralPath $absoluteOutDir) {
        if (Get-ChildItem -LiteralPath $absoluteOutDir -Force | Select-Object -First 1) {
            throw "OutDir became non-empty while the benchmark was building: $absoluteOutDir"
        }
    } else {
        New-Item -ItemType Directory -Force -Path $absoluteOutDir | Out-Null
    }
    & $suiteExe --dataset $dataset --out-dir $absoluteOutDir --runtime-cli $cliExe --trials $Trials
    if ($LASTEXITCODE -ne 0) { throw 'AgentPay live-local benchmark failed' }
    Write-Host "AgentPay benchmark artifacts: $absoluteOutDir"
} finally {
    Pop-Location
}
