param(
    [ValidateRange(1, 1000)]
    [int]$Trials = 25,
    [string]$OutDir = ('.local-run\agentpay-benchmark-' + (Get-Date -Format 'yyyyMMddTHHmmss')),
    [string]$DatasetPath = '',
    [string]$BinDir = '.local-run\bin-agentpay-benchmark'
)

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $repo 'commerce-runtime'
$datasetRoot = [IO.Path]::GetFullPath((Join-Path $runtimeDir 'testdata\s11\agentpay-benchmark'))
if ([string]::IsNullOrWhiteSpace($DatasetPath)) {
    $dataset = Join-Path $datasetRoot 'scenarios.jsonl'
} elseif ([IO.Path]::IsPathRooted($DatasetPath)) {
    $dataset = [IO.Path]::GetFullPath($DatasetPath)
} else {
    $dataset = [IO.Path]::GetFullPath((Join-Path $repo $DatasetPath))
}
$datasetPrefix = $datasetRoot.TrimEnd('\') + '\'
if (-not (Test-Path -LiteralPath $dataset -PathType Leaf) -or
    -not $dataset.StartsWith($datasetPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'DatasetPath must name an existing file under commerce-runtime/testdata/s11/agentpay-benchmark.'
}
$absoluteOutDir = [IO.Path]::GetFullPath((Join-Path $repo $OutDir))
$absoluteBinDir = [IO.Path]::GetFullPath((Join-Path $repo $BinDir))
$repoPrefix = [IO.Path]::GetFullPath($repo).TrimEnd('\') + '\'
if (-not $absoluteOutDir.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'OutDir must resolve inside the StablePay repository.'
}
if (-not $absoluteBinDir.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'BinDir must resolve inside the StablePay repository.'
}
if (Test-Path -LiteralPath $absoluteOutDir) {
    if (Get-ChildItem -LiteralPath $absoluteOutDir -Force | Select-Object -First 1) {
        throw "OutDir is not empty; choose a new output directory: $absoluteOutDir"
    }
}

$binDir = $absoluteBinDir
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$suiteExe = Join-Path $binDir 's11-live-local-suite.exe'
$cliExe = Join-Path $binDir 'stablepay-runtime.exe'
$sourceStatus = @(& git -C $repo -c "safe.directory=$repo" status --porcelain --untracked-files=all 2>$null)
if ($LASTEXITCODE -eq 0) {
    if ($sourceStatus.Count -gt 0) {
        $env:STABLEPAY_SOURCE_MODIFIED = 'true'
    } else {
        $env:STABLEPAY_SOURCE_MODIFIED = 'false'
    }
} else {
    Remove-Item Env:STABLEPAY_SOURCE_MODIFIED -ErrorAction SilentlyContinue
}
$sourceRevision = (& git -C $repo -c "safe.directory=$repo" rev-parse HEAD 2>$null | Out-String).Trim()
if ($LASTEXITCODE -eq 0 -and $sourceRevision -match '^[0-9a-fA-F]{40,64}$') {
    $env:STABLEPAY_SOURCE_REVISION = $sourceRevision.ToLowerInvariant()
} else {
    Remove-Item Env:STABLEPAY_SOURCE_REVISION -ErrorAction SilentlyContinue
}
$env:GOCACHE = Join-Path $repo '.local-run\gocache-agentpay-benchmark'
$env:GOMODCACHE = Join-Path $repo '.local-run\gomodcache-l2'
$env:GOSUMDB = 'off'
$env:GOPROXY = 'off'
$env:GOTOOLCHAIN = 'local'

Push-Location $runtimeDir
try {
    go build -buildvcs=false -o $suiteExe ./cmd/s11-live-local-suite
    if ($LASTEXITCODE -ne 0) { throw 'failed to build s11-live-local-suite' }
    go build -buildvcs=false -o $cliExe ./cmd/stablepay-runtime
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
