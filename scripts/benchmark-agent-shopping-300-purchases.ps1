param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeModule = Join-Path $repoRoot 'commerce-runtime'
$simulationRunner = Join-Path $repoRoot 'scripts\run-agent-shopping-simulation.ps1'
$merchantRunner = Join-Path $repoRoot 'scripts\start-simulated-merchants.ps1'
$merchantManifest = Join-Path $repoRoot '.local-run\simulated-merchants\manifest.json'
$merchantState = Join-Path $repoRoot '.local-run\simulated-merchants\processes.json'
$stateRoot = Join-Path $repoRoot '.local-run\agent-shopping'
$secretsPath = Join-Path $stateRoot 'secrets.env'
$intentRun = Join-Path $stateRoot 'evaluations\shopping-intents-20261010T135310.164825200Z'
$requiredPaths = @(
    $simulationRunner,
    $merchantRunner,
    $merchantManifest,
    $secretsPath,
    (Join-Path $intentRun 'suite.jsonl'),
    (Join-Path $intentRun 'intent-metrics.json'),
    (Join-Path $intentRun 'intent-trials.jsonl')
)
foreach ($path in $requiredPaths) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required isolated benchmark input is missing: $path"
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

function Get-MerchantHealthCount {
    $manifest = Get-Content -LiteralPath $merchantManifest -Raw -Encoding UTF8 | ConvertFrom-Json
    $healthy = 0
    foreach ($merchant in $manifest.merchants) {
        try {
            $null = Invoke-RestMethod -Uri "http://127.0.0.1:$($merchant.port)/healthz" -TimeoutSec 2
            $healthy++
        } catch { }
    }
    return $healthy
}

function Get-MerchantListenerCount {
    $manifest = Get-Content -LiteralPath $merchantManifest -Raw -Encoding UTF8 | ConvertFrom-Json
    $ports = @($manifest.merchants | ForEach-Object { [int]$_.port })
    $listeners = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
        Where-Object { $ports -contains $_.LocalPort } |
        Select-Object -ExpandProperty LocalPort -Unique)
    return $listeners.Count
}

$environmentNames = @(
    'SHOP_SIM_DB_PASSWORD',
    'SHOP_SIM_MYSQL_HTTP_300_PURCHASE_REPLAY',
    'SHOP_SIM_LOCKED_SUITE_PATH',
    'SHOP_SIM_INTENT_METRICS_PATH',
    'SHOP_SIM_SOURCE_MANIFEST_PATH',
    'SHOP_SIM_REPO_ROOT',
    'GOCACHE',
    'GOFLAGS'
)
$previousEnvironment = @{}
foreach ($name in $environmentNames) {
    $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$merchantsStartedByThisRun = $false
$testExit = 1
try {
    & powershell -NoProfile -ExecutionPolicy Bypass -File $simulationRunner -StartMySQL
    if ($LASTEXITCODE -ne 0) {
        throw "Could not start or verify the labeled isolated MySQL container (exit $LASTEXITCODE)."
    }

    $listeners = Get-MerchantListenerCount
    if ($listeners -eq 0) {
        if (Test-Path -LiteralPath $merchantState -PathType Leaf) {
            throw 'A simulated Merchant process-state file exists but no APIs are healthy; inspect it before starting or stopping processes.'
        }
        & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantRunner
        if ($LASTEXITCODE -ne 0) {
            throw "Could not start the isolated simulated Merchant fixtures (exit $LASTEXITCODE)."
        }
        $merchantsStartedByThisRun = $true
    }
	elseif ($listeners -ne 12) {
		throw "Expected zero or twelve Merchant listeners; observed $listeners. Refusing to start or stop a partial set."
	}
	$healthy = Get-MerchantHealthCount
    if ($healthy -ne 12) {
        throw "Expected all 12 Merchant fixtures healthy; observed $healthy."
    }

    $env:SHOP_SIM_DB_PASSWORD = $dbPassword
    $env:SHOP_SIM_MYSQL_HTTP_300_PURCHASE_REPLAY = '1'
    $env:SHOP_SIM_LOCKED_SUITE_PATH = Join-Path $intentRun 'suite.jsonl'
    $env:SHOP_SIM_INTENT_METRICS_PATH = Join-Path $intentRun 'intent-metrics.json'
    $env:SHOP_SIM_SOURCE_MANIFEST_PATH = $merchantManifest
    $env:SHOP_SIM_REPO_ROOT = $repoRoot
    $env:GOCACHE = Join-Path $repoRoot '.local-run\codex-go-build-cache'
    $env:GOFLAGS = '-buildvcs=false'

    Push-Location $runtimeModule
    try {
        & go test ./benchmarks/agentshopping -run '^TestMySQLHTTP300PurchaseReplayFaults$' -count=1 -timeout 45m -v
        $testExit = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
}
finally {
    try {
        if ($merchantsStartedByThisRun) {
            & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantRunner -Stop
            if ($LASTEXITCODE -ne 0) {
                throw "Could not stop only the simulated Merchant processes started by this run (exit $LASTEXITCODE)."
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
    throw "The 300-attempt isolated purchase/replay benchmark failed (exit $testExit)."
}
Write-Output '300 isolated mock purchase/replay trials passed; MySQL remains available and its data was preserved.'
