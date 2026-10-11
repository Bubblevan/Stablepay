param(
    [ValidateRange(1, 143)]
    [int]$MySQLPoolMaxOpen = 24,
    [ValidateNotNullOrEmpty()]
    [int[]]$ConcurrencyLevels = @(1, 10, 25, 50, 100, 200)
)

$ErrorActionPreference = 'Stop'
$allowedConcurrencyLevels = @(1, 10, 25, 50, 100, 200)
if ($ConcurrencyLevels.Count -eq 0 -or @($ConcurrencyLevels | Where-Object { $_ -notin $allowedConcurrencyLevels }).Count -gt 0) {
    throw 'ConcurrencyLevels must select from 1, 10, 25, 50, 100, 200.'
}
if (@($ConcurrencyLevels | Sort-Object -Unique).Count -ne $ConcurrencyLevels.Count) {
    throw 'ConcurrencyLevels cannot contain duplicates.'
}
$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeModule = Join-Path $repoRoot 'commerce-runtime'
$goTempDir = Join-Path $repoRoot '.local-run\agent-shopping\go-tmp-concurrent'
New-Item -ItemType Directory -Force -Path $goTempDir | Out-Null
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

$systemRoot = if (-not [string]::IsNullOrWhiteSpace($env:SystemRoot)) {
    $env:SystemRoot
}
else {
    [Environment]::GetFolderPath([Environment+SpecialFolder]::System)
}
$systemDriveName = [IO.Path]::GetPathRoot($systemRoot).TrimEnd([char]'\').TrimEnd([char]':')
$repoDriveName = [IO.Path]::GetPathRoot($repoRoot).TrimEnd([char]'\').TrimEnd([char]':')
$minimumFreeBytes = 10GB
$minimumAvailableRAMBytes = 8GB
function Get-DriveFreeBytes {
    param([Parameter(Mandatory = $true)][string]$DriveName)

    if ($DriveName -notmatch '^[A-Za-z]$') {
        throw "Could not resolve a drive letter from '$DriveName'."
    }
    $drive = [System.IO.DriveInfo]::new("${DriveName}:\")
    if (-not $drive.IsReady) {
        throw "Drive ${DriveName}: is not ready."
    }
    return [long]$drive.AvailableFreeSpace
}
$systemDriveFreeBytes = Get-DriveFreeBytes -DriveName $systemDriveName
$repositoryDriveFreeBytes = Get-DriveFreeBytes -DriveName $repoDriveName
if ($systemDriveFreeBytes -lt $minimumFreeBytes) {
    $freeGB = [math]::Round($systemDriveFreeBytes / 1GB, 1)
    throw "Refusing benchmark: system drive free space is $freeGB GB; require at least 10 GB."
}
if ($repositoryDriveFreeBytes -lt $minimumFreeBytes) {
    $freeGB = [math]::Round($repositoryDriveFreeBytes / 1GB, 1)
    throw "Refusing benchmark: repository drive free space is $freeGB GB; require at least 10 GB."
}
try {
    $availableRAMMB = (Get-Counter -Counter '\Memory\Available MBytes' -MaxSamples 1 -ErrorAction Stop).CounterSamples[0].CookedValue
}
catch {
    throw 'Refusing benchmark: available host RAM cannot be measured through the Windows memory counter.'
}
$availableRAMBytes = [double]$availableRAMMB * 1MB
if ($availableRAMBytes -lt $minimumAvailableRAMBytes) {
    throw "Refusing benchmark: available host RAM is $([math]::Round($availableRAMBytes / 1GB, 1)) GB; require at least 8 GB."
}
$availableRAMGB = [math]::Round($availableRAMBytes / 1GB, 1)
$systemDriveFreeGB = [math]::Round($systemDriveFreeBytes / 1GB, 1)
$repositoryDriveFreeGB = [math]::Round($repositoryDriveFreeBytes / 1GB, 1)
Write-Output "LOCAL_RESOURCE_PREFLIGHT system_drive=$systemDriveName free_gb=$systemDriveFreeGB repo_drive=$repoDriveName repo_free_gb=$repositoryDriveFreeGB available_ram_gb=$availableRAMGB min_drive_free_gb=10 min_available_ram_gb=8"

$mysqlProbe = [System.Net.Sockets.TcpClient]::new()
try {
    $connect = $mysqlProbe.BeginConnect('127.0.0.1', 13327, $null, $null)
    if (-not $connect.AsyncWaitHandle.WaitOne(1000)) {
        throw 'The isolated simulation MySQL is not listening on 127.0.0.1:13327.'
    }
    $mysqlProbe.EndConnect($connect)
}
catch {
    throw 'The isolated simulation MySQL must already be listening on 127.0.0.1:13327; no Merchant fixtures were started.'
}
finally {
    $mysqlProbe.Close()
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
    'SHOP_SIM_MYSQL_HTTP_CONCURRENT_PREFLIGHT',
    'SHOP_SIM_HOST_AVAILABLE_RAM_GB',
    'SHOP_SIM_HOST_SYSTEM_DISK_FREE_GB',
    'SHOP_SIM_HOST_REPOSITORY_DISK_FREE_GB',
    'SHOP_SIM_MYSQL_POOL_MAX_OPEN',
    'SHOP_SIM_HTTP_CONCURRENCY_LEVELS',
    'SHOP_SIM_REPO_ROOT',
    'SHOP_SIM_LOCKED_SUITE_PATH',
    'SHOP_SIM_INTENT_METRICS_PATH',
    'SHOP_SIM_SOURCE_MANIFEST_PATH',
    'GOCACHE',
    'GOTMPDIR',
    'TEMP',
    'TMP',
    'GOFLAGS',
    'GOPROXY'
)
$previousEnvironment = @{}
foreach ($name in $environmentNames) {
    $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

$merchantStarted = $false
$testExit = 1
try {
    $env:GOCACHE = Join-Path $repoRoot '.local-run\go-build-cache'
    $env:GOTMPDIR = $goTempDir
    $env:TEMP = $goTempDir
    $env:TMP = $goTempDir
    $env:GOFLAGS = '-mod=readonly -buildvcs=false -p=2'
    $env:GOPROXY = 'off'
    $env:SHOP_SIM_DB_PASSWORD = $dbPassword
    $env:SHOP_SIM_MYSQL_POOL_MAX_OPEN = [string]$MySQLPoolMaxOpen
    $env:SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD = '0'
    $env:SHOP_SIM_MYSQL_HTTP_CONCURRENT_PREFLIGHT = '1'
    $invariantCulture = [Globalization.CultureInfo]::InvariantCulture
    $env:SHOP_SIM_HOST_AVAILABLE_RAM_GB = ([double]$availableRAMGB).ToString('0.0', $invariantCulture)
    $env:SHOP_SIM_HOST_SYSTEM_DISK_FREE_GB = ([double]$systemDriveFreeGB).ToString('0.0', $invariantCulture)
    $env:SHOP_SIM_HOST_REPOSITORY_DISK_FREE_GB = ([double]$repositoryDriveFreeGB).ToString('0.0', $invariantCulture)

    Push-Location $runtimeModule
    try {
        & go test ./benchmarks/agentshopping -run '^TestMySQLHTTPConcurrentLoadConnectionPreflight$' -count=1 -timeout 2m -v
        $preflightExit = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }
    if ($preflightExit -ne 0) {
        throw "MySQL connection preflight failed (exit $preflightExit); no Merchant fixtures were started."
    }

    $env:SHOP_SIM_MYSQL_HTTP_CONCURRENT_PREFLIGHT = '0'
    & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantScript
    if ($LASTEXITCODE -ne 0) {
        throw "Could not start the simulated Merchant fixtures (exit $LASTEXITCODE)."
    }
    $merchantStarted = $true

    $systemDriveFreeBytes = Get-DriveFreeBytes -DriveName $systemDriveName
    $repositoryDriveFreeBytes = Get-DriveFreeBytes -DriveName $repoDriveName
    $availableRAMMB = (Get-Counter -Counter '\Memory\Available MBytes' -MaxSamples 1 -ErrorAction Stop).CounterSamples[0].CookedValue
    $availableRAMBytes = [double]$availableRAMMB * 1MB
    if ($systemDriveFreeBytes -lt $minimumFreeBytes -or $repositoryDriveFreeBytes -lt $minimumFreeBytes -or $availableRAMBytes -lt $minimumAvailableRAMBytes) {
        throw "Refusing concurrent load after Merchant startup: free_gb=$([math]::Round($systemDriveFreeBytes/1GB,1))/$([math]::Round($repositoryDriveFreeBytes/1GB,1)) available_ram_gb=$([math]::Round($availableRAMBytes/1GB,1)); minimums are 10/10/8 GB. Fixtures will be stopped."
    }
    $availableRAMGB = [math]::Round($availableRAMBytes / 1GB, 1)
    $systemDriveFreeGB = [math]::Round($systemDriveFreeBytes / 1GB, 1)
    $repositoryDriveFreeGB = [math]::Round($repositoryDriveFreeBytes / 1GB, 1)
    $env:SHOP_SIM_HOST_AVAILABLE_RAM_GB = ([double]$availableRAMGB).ToString('0.0', $invariantCulture)
    $env:SHOP_SIM_HOST_SYSTEM_DISK_FREE_GB = ([double]$systemDriveFreeGB).ToString('0.0', $invariantCulture)
    $env:SHOP_SIM_HOST_REPOSITORY_DISK_FREE_GB = ([double]$repositoryDriveFreeGB).ToString('0.0', $invariantCulture)
    Write-Output "LOCAL_RESOURCE_PREFLIGHT_AFTER_MERCHANT system_drive=$systemDriveName free_gb=$systemDriveFreeGB repo_drive=$repoDriveName repo_free_gb=$repositoryDriveFreeGB available_ram_gb=$availableRAMGB"

    $env:SHOP_SIM_DB_PASSWORD = $dbPassword
    $env:SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD = '1'
    $env:SHOP_SIM_MYSQL_POOL_MAX_OPEN = [string]$MySQLPoolMaxOpen
    $env:SHOP_SIM_HTTP_CONCURRENCY_LEVELS = $ConcurrencyLevels -join ','
    $env:SHOP_SIM_REPO_ROOT = $repoRoot
    $env:SHOP_SIM_LOCKED_SUITE_PATH = Join-Path $evaluationRoot 'suite.jsonl'
    $env:SHOP_SIM_INTENT_METRICS_PATH = Join-Path $evaluationRoot 'intent-metrics.json'
    $env:SHOP_SIM_SOURCE_MANIFEST_PATH = $merchantManifest
    $env:GOFLAGS = '-mod=readonly -buildvcs=false -p=2'
    $env:GOPROXY = 'off'

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
Write-Output "Concurrent Agent-shopping benchmark passed with MySQL max-open connections=$MySQLPoolMaxOpen and tiers=$($ConcurrencyLevels -join ',')."
