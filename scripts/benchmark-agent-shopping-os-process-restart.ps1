param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeModule = Join-Path $repoRoot 'commerce-runtime'
$stateRoot = Join-Path $repoRoot '.local-run\agent-shopping'
$secretsPath = Join-Path $stateRoot 'secrets.env'
$intentRun = Join-Path $stateRoot 'evaluations\shopping-intents-20261010T135310.164825200Z'
$merchantManifest = Join-Path $repoRoot '.local-run\simulated-merchants\manifest.json'
$requiredPaths = @(
    $secretsPath,
    $merchantManifest,
    (Join-Path $intentRun 'suite.jsonl'),
    (Join-Path $intentRun 'intent-metrics.json'),
    (Join-Path $intentRun 'intent-trials.jsonl')
)
foreach ($path in $requiredPaths) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required local benchmark input is missing: $path"
    }
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
if (-not (Test-NetConnection 127.0.0.1 -Port 13327 -InformationLevel Quiet -WarningAction SilentlyContinue)) {
    throw 'The isolated MySQL listener at 127.0.0.1:13327 is not available; refusing to start Docker or download an image.'
}

$environmentNames = @(
    'SHOP_SIM_DB_PASSWORD',
    'SHOP_SIM_MYSQL_HTTP_PROCESS_RESTART',
    'SHOP_SIM_LOCKED_SUITE_PATH',
    'SHOP_SIM_INTENT_METRICS_PATH',
    'SHOP_SIM_SOURCE_MANIFEST_PATH',
    'SHOP_SIM_REPO_ROOT',
    'GOCACHE',
    'GOFLAGS',
    'GOPROXY',
    'GOMAXPROCS'
)
$previousEnvironment = @{}
foreach ($name in $environmentNames) {
    $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$testExit = 1
try {
    $env:SHOP_SIM_DB_PASSWORD = $dbPassword
    $env:SHOP_SIM_MYSQL_HTTP_PROCESS_RESTART = '1'
    $env:SHOP_SIM_LOCKED_SUITE_PATH = Join-Path $intentRun 'suite.jsonl'
    $env:SHOP_SIM_INTENT_METRICS_PATH = Join-Path $intentRun 'intent-metrics.json'
    $env:SHOP_SIM_SOURCE_MANIFEST_PATH = $merchantManifest
    $env:SHOP_SIM_REPO_ROOT = $repoRoot
    $env:GOCACHE = Join-Path $repoRoot '.local-run\go-build-cache'
    $env:GOFLAGS = '-buildvcs=false'
    $env:GOPROXY = 'off'
    $env:GOMAXPROCS = '2'

    Push-Location $runtimeModule
    try {
        & go test ./benchmarks/agentshopping -run '^TestMySQLHTTPOSProcessRestartPurchaseReplay$' -count=1 -timeout 20m -v
        $testExit = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}
finally {
    foreach ($name in $environmentNames) {
        [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
    }
}

exit $testExit
