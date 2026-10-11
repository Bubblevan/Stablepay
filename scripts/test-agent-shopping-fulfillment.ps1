param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeModule = Join-Path $repoRoot 'commerce-runtime'
$secretPath = Join-Path $repoRoot '.local-run\agent-shopping\secrets.env'
$localRunRoot = Join-Path $repoRoot '.local-run'

if (-not (Test-Path -LiteralPath $secretPath -PathType Leaf)) {
    throw 'The isolated shopping benchmark secret file is missing; this runner does not create credentials.'
}
if (-not (Test-NetConnection 127.0.0.1 -Port 13327 -InformationLevel Quiet -WarningAction SilentlyContinue)) {
    throw 'Isolated MySQL is not listening at 127.0.0.1:13327; start it separately. This runner will not start Docker or pull images.'
}

$dbPassword = $null
foreach ($line in [IO.File]::ReadAllLines($secretPath)) {
    if ($line.StartsWith('SHOP_SIM_DB_PASSWORD=')) {
        $dbPassword = $line.Substring('SHOP_SIM_DB_PASSWORD='.Length)
        break
    }
}
if ([string]::IsNullOrWhiteSpace($dbPassword)) {
    throw 'The isolated benchmark secret file does not contain SHOP_SIM_DB_PASSWORD.'
}

$environmentNames = @(
    'SHOP_SIM_DB_PASSWORD', 'SHOP_SIM_MYSQL_INTEGRATION', 'SHOP_SIM_MYSQL_SCALE_INTEGRATION',
    'SHOP_SIM_MYSQL_HTTP_LIFECYCLE', 'SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD',
    'SHOP_SIM_MYSQL_FULL_LIFECYCLE', 'SHOP_SIM_MYSQL_HTTP_300_PURCHASE_REPLAY',
    'SHOP_SIM_MYSQL_HTTP_PROCESS_RESTART', 'SHOP_SIM_MYSQL_HTTP_FULFILLMENT_RESTART', 'SHOP_SIM_MYSQL_CLEANUP_TENANT',
    'GOCACHE', 'GOPROXY', 'GOFLAGS', 'GOMAXPROCS'
)
$previousEnvironment = @{}
foreach ($name in $environmentNames) {
    $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$cachePath = Join-Path $localRunRoot ('go-cache-fulfillment-test-' + [Guid]::NewGuid().ToString('N'))
$testExit = 1
try {
    $null = New-Item -ItemType Directory -Path $localRunRoot -Force
    $null = New-Item -ItemType Directory -Path $cachePath
    $env:SHOP_SIM_DB_PASSWORD = $dbPassword
    $env:SHOP_SIM_MYSQL_INTEGRATION = '1'
    $env:SHOP_SIM_MYSQL_SCALE_INTEGRATION = '0'
    $env:SHOP_SIM_MYSQL_HTTP_LIFECYCLE = '0'
    $env:SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD = '0'
    $env:SHOP_SIM_MYSQL_FULL_LIFECYCLE = '0'
    $env:SHOP_SIM_MYSQL_HTTP_300_PURCHASE_REPLAY = '0'
    $env:SHOP_SIM_MYSQL_HTTP_PROCESS_RESTART = '0'
    $env:SHOP_SIM_MYSQL_HTTP_FULFILLMENT_RESTART = '1'
    $env:SHOP_SIM_MYSQL_CLEANUP_TENANT = ''
    $env:GOCACHE = $cachePath
    $env:GOPROXY = 'off'
    $env:GOFLAGS = '-mod=readonly -p=2'
    $env:GOMAXPROCS = '2'

    Push-Location $runtimeModule
    try {
        & go test ./benchmarks/agentshopping -run '^TestMySQLHTTP(FulfillmentIsIdempotentAndCompensatesDeliveryFailure|FulfillmentRetryResumesAfterProcessRestart)$' -count=1 -v
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
    $repoFull = [IO.Path]::GetFullPath($repoRoot)
    $cacheFull = [IO.Path]::GetFullPath($cachePath)
    $localRunFull = [IO.Path]::GetFullPath($localRunRoot).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $cacheFull.StartsWith($localRunFull, [StringComparison]::OrdinalIgnoreCase) -or
        -not $cacheFull.StartsWith($repoFull, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove a Go cache outside this repository's .local-run directory: $cacheFull"
    }
    if (Test-Path -LiteralPath $cacheFull) {
        Remove-Item -LiteralPath $cacheFull -Recurse -Force
    }
}

if ($testExit -ne 0) {
    throw "The isolated fulfillment integration test failed (exit $testExit)."
}
Write-Output 'Fulfillment idempotency, durable transient retry across API process restart, permanent-failure refund, inventory restock, and decline guard passed against the existing isolated MySQL schema.'
