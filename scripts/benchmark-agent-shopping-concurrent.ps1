param(
    [ValidateRange(1, 128)]
    [int]$MySQLPoolMaxOpen = 24
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeModule = Join-Path $repoRoot 'commerce-runtime'
$merchantScript = Join-Path $repoRoot 'scripts\start-simulated-merchants.ps1'
$merchantState = Join-Path $repoRoot '.local-run\simulated-merchants\processes.json'
$secretsPath = Join-Path $repoRoot '.local-run\agent-shopping\secrets.env'
$evaluationRoot = Join-Path $repoRoot '.local-run\agent-shopping\evaluations\shopping-intents-20261010T135310.164825200Z'
$merchantManifest = Join-Path $repoRoot '.local-run\simulated-merchants\manifest.json'
$requiredPaths = @(
    $merchantScript,
    $secretsPath,
    (Join-Path $evaluationRoot 'suite.jsonl'),
    (Join-Path $evaluationRoot 'intent-metrics.json'),
    (Join-Path $evaluationRoot 'intent-trials.jsonl'),
    $merchantManifest
)
foreach ($path in $requiredPaths) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required local benchmark input is missing: $path"
    }
}
if (Test-Path -LiteralPath $merchantState -PathType Leaf) {
    throw 'A simulated Merchant process state already exists; refusing to take ownership of existing processes.'
}

$dbPassword = $null
foreach ($line in [IO.File]::ReadAllLines($secretsPath)) {
    if ($line.StartsWith('SHOP_SIM_DB_PASSWORD=')) {
        $dbPassword = $line.Substring('SHOP_SIM_DB_PASSWORD='.Length)
        break
    }
}
if ([string]::IsNullOrWhiteSpace($dbPassword)) {
    throw 'The isolated simulation secret file does not contain SHOP_SIM_DB_PASSWORD.'
}

$environmentNames = @(
    'SHOP_SIM_DB_PASSWORD',
    'SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD',
    'SHOP_SIM_MYSQL_POOL_MAX_OPEN',
    'SHOP_SIM_REPO_ROOT',
    'SHOP_SIM_LOCKED_SUITE_PATH',
    'SHOP_SIM_INTENT_METRICS_PATH',
    'SHOP_SIM_SOURCE_MANIFEST_PATH',
    'GOCACHE',
    'GOFLAGS'
)
$previousEnvironment = @{}
foreach ($name in $environmentNames) {
    $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$merchantStarted = $false
$testExit = 1
try {
    $env:GOFLAGS = '-buildvcs=false'
    & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantScript
    if ($LASTEXITCODE -ne 0) {
        throw "Could not start the simulated Merchant fixtures (exit $LASTEXITCODE)."
    }
    $merchantStarted = $true

    $env:SHOP_SIM_DB_PASSWORD = $dbPassword
    $env:SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD = '1'
    $env:SHOP_SIM_MYSQL_POOL_MAX_OPEN = [string]$MySQLPoolMaxOpen
    $env:SHOP_SIM_REPO_ROOT = $repoRoot
    $env:SHOP_SIM_LOCKED_SUITE_PATH = Join-Path $evaluationRoot 'suite.jsonl'
    $env:SHOP_SIM_INTENT_METRICS_PATH = Join-Path $evaluationRoot 'intent-metrics.json'
    $env:SHOP_SIM_SOURCE_MANIFEST_PATH = $merchantManifest
    $env:GOCACHE = Join-Path $repoRoot '.local-run\go-build-cache'
    $env:GOFLAGS = '-buildvcs=false'

    Push-Location $runtimeModule
    try {
        & go test ./benchmarks/agentshopping -run '^TestMySQLHTTPConcurrentDiscoveryLoad$' -count=1 -timeout 30m -v
        $testExit = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}
finally {
    try {
        if ($merchantStarted) {
            & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantScript -Stop
            if ($LASTEXITCODE -ne 0) {
                throw "Could not cleanly stop the simulated Merchant fixtures (exit $LASTEXITCODE)."
            }
        }
    }
    finally {
        foreach ($name in $environmentNames) {
            [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
        }
    }
}

if ($testExit -ne 0) {
    throw "Concurrent Agent-shopping benchmark failed (exit $testExit)."
}
Write-Output "Concurrent Agent-shopping benchmark passed with MySQL max-open connections=$MySQLPoolMaxOpen."
