param(
    [switch]$StartMySQL,
    [switch]$StartMerchants,
    [switch]$Seed,
    [switch]$StartAPI,
    [switch]$UseMerchantHTTPSource,
    [switch]$StopAPI,
    [switch]$StopMySQL,
    [switch]$StopMerchants,
    [switch]$Status
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$stateRoot = Join-Path $repoRoot '.local-run\agent-shopping'
$secretPath = Join-Path $stateRoot 'secrets.env'
$dataPath = Join-Path $stateRoot 'mysql-data'
$apiStatePath = Join-Path $stateRoot 'api-process.json'
$containerName = 'stablepay-agent-shopping-sim-20261010'
$mysqlImage = 'mysql:8.0'
$apiAddr = '127.0.0.1:18090'
$merchantScript = Join-Path $repoRoot 'scripts\start-simulated-merchants.ps1'
$manifestPath = Join-Path $repoRoot '.local-run\simulated-merchants\manifest.json'
$binaryPath = Join-Path $stateRoot 'agentshopping-sim.exe'

function Assert-RepoLocalPath([string]$path, [string]$root) {
    $fullPath = [IO.Path]::GetFullPath($path)
    $fullRoot = [IO.Path]::GetFullPath($root).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $fullPath.StartsWith($fullRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing path outside intended local-run directory: $fullPath"
    }
    return $fullPath
}

function Get-SimulationSecrets {
    if (-not (Test-Path -LiteralPath $secretPath -PathType Leaf)) {
        $null = New-Item -ItemType Directory -Path (Assert-RepoLocalPath $stateRoot (Join-Path $repoRoot '.local-run')) -Force
        $rootBytes = New-Object byte[] 32
        $dbBytes = New-Object byte[] 32
        $apiBytes = New-Object byte[] 32
        $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
        try {
            $rng.GetBytes($rootBytes)
            $rng.GetBytes($dbBytes)
            $rng.GetBytes($apiBytes)
        } finally {
            $rng.Dispose()
        }
        $rootPassword = [BitConverter]::ToString($rootBytes).Replace('-', '').ToLowerInvariant()
        $dbPassword = [BitConverter]::ToString($dbBytes).Replace('-', '').ToLowerInvariant()
        $apiKey = [BitConverter]::ToString($apiBytes).Replace('-', '').ToLowerInvariant()
        $secretText = @(
            "MYSQL_ROOT_PASSWORD=$rootPassword",
            'MYSQL_DATABASE=stablepay_shopping_sim',
            'MYSQL_USER=stablepay_sim',
            "MYSQL_PASSWORD=$dbPassword",
            "SHOP_SIM_DB_PASSWORD=$dbPassword",
            "SHOP_SIM_API_KEY=$apiKey"
        ) -join "`n"
        [IO.File]::WriteAllText($secretPath, $secretText + "`n", [Text.UTF8Encoding]::new($false))
    }

    $values = @{}
    foreach ($line in [IO.File]::ReadAllLines($secretPath)) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        $separator = $line.IndexOf('=')
        if ($separator -lt 1) { throw 'Local simulation secrets file has an invalid line.' }
        $values[$line.Substring(0, $separator)] = $line.Substring($separator + 1)
    }
    foreach ($required in @('MYSQL_ROOT_PASSWORD', 'MYSQL_DATABASE', 'MYSQL_USER', 'MYSQL_PASSWORD', 'SHOP_SIM_DB_PASSWORD', 'SHOP_SIM_API_KEY')) {
        if (-not $values.ContainsKey($required) -or [string]::IsNullOrWhiteSpace($values[$required])) {
            throw "Local simulation secrets file is missing $required. It was not printed or copied."
        }
    }
    if ($values['MYSQL_DATABASE'] -ne 'stablepay_shopping_sim' -or $values['MYSQL_USER'] -ne 'stablepay_sim' -or
        $values['SHOP_SIM_DB_PASSWORD'] -ne $values['MYSQL_PASSWORD'] -or $values['SHOP_SIM_API_KEY'].Length -lt 32) {
        throw 'Local simulation credentials do not match the isolated database configuration.'
    }
    return $values
}

function Invoke-Docker([string[]]$arguments) {
    & docker @arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker operation failed (exit $LASTEXITCODE)." }
}

function Get-ContainerState {
	$containerId = & docker container ls --all --quiet --filter "name=^/$containerName$"
	if ($LASTEXITCODE -ne 0) { throw 'Could not query Docker container state.' }
	if ([string]::IsNullOrWhiteSpace(($containerId -join ''))) { return $null }
	$result = & docker inspect --format '{{json .Config.Labels}}|{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' $containerId
	if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace(($result -join ''))) { throw 'Could not inspect the matching simulation container.' }
    $parts = ($result -join '') -split '\|', 3
    $labels = $parts[0] | ConvertFrom-Json
    return [pscustomobject]@{ Labels = $labels; Status = $parts[1]; Health = $parts[2] }
}

function Ensure-MySQL {
    $null = Get-SimulationSecrets
    $null = New-Item -ItemType Directory -Path (Assert-RepoLocalPath $dataPath (Join-Path $repoRoot '.local-run')) -Force
    $existing = Get-ContainerState
    if ($null -ne $existing) {
        if ($existing.Labels.'com.stablepay.simulation' -ne 'agent-shopping') {
            throw "Container name $containerName is occupied by an unrelated container; refusing to touch it."
        }
        if ($existing.Status -ne 'running') { Invoke-Docker @('start', $containerName) }
    } else {
        & docker image inspect $mysqlImage *> $null
        if ($LASTEXITCODE -ne 0) { throw "$mysqlImage is not cached locally; refusing an implicit image pull." }
        $listener = Get-NetTCPConnection -State Listen -LocalPort 13327 -ErrorAction SilentlyContinue
        if ($listener) { throw '127.0.0.1:13327 is already listening; no container was started.' }
        $resolvedDataPath = Assert-RepoLocalPath $dataPath (Join-Path $repoRoot '.local-run')
        Invoke-Docker @(
            'run', '--detach', '--name', $containerName,
            '--label', 'com.stablepay.simulation=agent-shopping',
            '--publish', '127.0.0.1:13327:3306',
            '--volume', "${resolvedDataPath}:/var/lib/mysql",
            '--env-file', $secretPath,
            '--health-cmd', 'mysqladmin ping -h 127.0.0.1 --silent',
            '--health-interval', '5s', '--health-timeout', '3s', '--health-retries', '24',
            $mysqlImage
        ) | Out-Null
    }
    $deadline = [DateTime]::UtcNow.AddMinutes(2)
    do {
        $state = Get-ContainerState
        if ($null -eq $state) { throw 'Isolated MySQL container disappeared during startup.' }
        if ($state.Health -eq 'healthy') { return }
        if ($state.Status -ne 'running' -or $state.Health -eq 'unhealthy') {
            throw "Isolated MySQL container did not become healthy (status=$($state.Status), health=$($state.Health))."
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Timed out waiting for isolated MySQL readiness.'
}

function Test-MerchantHealth {
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { return 0 }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $healthy = 0
    foreach ($merchant in $manifest.merchants) {
        try {
            $null = Invoke-RestMethod -Uri "http://127.0.0.1:$($merchant.port)/healthz" -TimeoutSec 2
            $healthy = $healthy + 1
        } catch { }
    }
    return $healthy
}

function Ensure-Merchants {
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw "Synthetic Merchant manifest not found: $manifestPath"
    }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $healthy = Test-MerchantHealth
    if ($healthy -eq @($manifest.merchants).Count) { return }
    if ($healthy -ne 0) { throw "Only $healthy of $(@($manifest.merchants).Count) simulated Merchants are healthy; refusing to start duplicates." }
    & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantScript
    if ($LASTEXITCODE -ne 0) { throw 'Could not start the isolated simulated Merchant fixtures.' }
}

function Build-SimulationBinary {
    $null = New-Item -ItemType Directory -Path (Assert-RepoLocalPath $stateRoot (Join-Path $repoRoot '.local-run')) -Force
    Push-Location (Join-Path $repoRoot 'commerce-runtime')
    try {
        & go build -o $binaryPath ./cmd/agentshopping-sim
        if ($LASTEXITCODE -ne 0) { throw "agentshopping-sim build failed (exit $LASTEXITCODE)." }
    } finally {
        Pop-Location
    }
}

function Invoke-Seed {
    Ensure-MySQL
    Ensure-Merchants
    Build-SimulationBinary
    $secrets = Get-SimulationSecrets
    $oldDbPassword = $env:SHOP_SIM_DB_PASSWORD
    try {
        $env:SHOP_SIM_DB_PASSWORD = $secrets['SHOP_SIM_DB_PASSWORD']
        & $binaryPath seed --manifest $manifestPath --seed 20261010 --buyers 2000 --tasks 500
        if ($LASTEXITCODE -ne 0) { throw "Seed command failed (exit $LASTEXITCODE)." }
    } finally {
        $env:SHOP_SIM_DB_PASSWORD = $oldDbPassword
    }
}

function Start-SimulationAPI {
    Ensure-MySQL
    Build-SimulationBinary
    if ($UseMerchantHTTPSource) {
        $merchantHealth = Test-MerchantHealth
        if ($merchantHealth -ne 12) { throw "Merchant HTTP source requested, but only $merchantHealth of 12 fixture APIs are healthy." }
        if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Merchant manifest not found: $manifestPath" }
    }
    if (Test-Path -LiteralPath $apiStatePath -PathType Leaf) {
        $state = Get-Content -LiteralPath $apiStatePath -Raw | ConvertFrom-Json
        $process = Get-Process -Id ([int]$state.pid) -ErrorAction SilentlyContinue
        if ($null -ne $process) { throw "Recorded simulation API process is already present (PID $($state.pid))." }
        throw 'Stale API process record exists; inspect it before removing it.'
    }
    $listener = Get-NetTCPConnection -State Listen -LocalPort 18090 -ErrorAction SilentlyContinue
    if ($listener) { throw '127.0.0.1:18090 is already listening; no API process was started.' }
    $secrets = Get-SimulationSecrets
    $stdoutPath = Join-Path $stateRoot 'api.stdout.log'
    $stderrPath = Join-Path $stateRoot 'api.stderr.log'
    $usagePath = Assert-RepoLocalPath (Join-Path $stateRoot 'model-usage') (Join-Path $repoRoot '.local-run')
    $oldDbPassword, $oldApiKey, $oldHttpAddr, $oldUsageDir, $oldMerchantManifest = $env:SHOP_SIM_DB_PASSWORD, $env:SHOP_SIM_API_KEY, $env:SHOP_SIM_HTTP_ADDR, $env:SHOP_SIM_USAGE_DIR, $env:SHOP_SIM_MERCHANT_MANIFEST
    try {
        $env:SHOP_SIM_DB_PASSWORD = $secrets['SHOP_SIM_DB_PASSWORD']
        $env:SHOP_SIM_API_KEY = $secrets['SHOP_SIM_API_KEY']
        $env:SHOP_SIM_HTTP_ADDR = $apiAddr
        $env:SHOP_SIM_USAGE_DIR = $usagePath
        if ($UseMerchantHTTPSource) { $env:SHOP_SIM_MERCHANT_MANIFEST = $manifestPath }
        else { $env:SHOP_SIM_MERCHANT_MANIFEST = '' }
        $process = Start-Process -FilePath $binaryPath -ArgumentList 'serve' -WorkingDirectory $stateRoot `
            -WindowStyle Hidden -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -PassThru
    } finally {
        $env:SHOP_SIM_DB_PASSWORD = $oldDbPassword
        $env:SHOP_SIM_API_KEY = $oldApiKey
        $env:SHOP_SIM_HTTP_ADDR = $oldHttpAddr
        $env:SHOP_SIM_USAGE_DIR = $oldUsageDir
        $env:SHOP_SIM_MERCHANT_MANIFEST = $oldMerchantManifest
    }
    $state = [pscustomobject]@{ binary = [IO.Path]::GetFullPath($binaryPath); pid = $process.Id; started_utc = $process.StartTime.ToUniversalTime().ToString('o') }
    $state | ConvertTo-Json | Set-Content -LiteralPath $apiStatePath -Encoding utf8
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $process.Refresh()
        if ($process.HasExited) { throw "Simulation API exited early; inspect $stderrPath (secrets are not logged)." }
        $ready = $null
        try {
            $ready = Invoke-RestMethod -Uri 'http://127.0.0.1:18090/readyz' -TimeoutSec 2
        } catch { }
        if ($null -ne $ready -and $ready.status -eq 'ready') {
            if ($UseMerchantHTTPSource) {
                $catalog = Invoke-RestMethod -Uri 'http://127.0.0.1:18090/v1/catalog/source' `
                    -Headers @{ 'X-Simulation-API-Key' = $secrets['SHOP_SIM_API_KEY'] } -TimeoutSec 5
                if ($catalog.source -ne 'merchant_http_snapshot' -or $catalog.merchant_count -ne 12 -or $catalog.product_count -ne 24000) {
                    throw "Shopping API did not confirm the expected live Merchant HTTP snapshot: $($catalog | ConvertTo-Json -Compress)"
                }
                $dataset = Invoke-RestMethod -Uri 'http://127.0.0.1:18090/v1/dataset' `
                    -Headers @{ 'X-Simulation-API-Key' = $secrets['SHOP_SIM_API_KEY'] } -TimeoutSec 5
                if ($dataset.counts.merchants -ne 12 -or $dataset.counts.products -ne 24000) {
                    throw "Isolated MySQL seed dimensions do not match the Merchant catalog: $($dataset.counts | ConvertTo-Json -Compress)"
                }
            }
            return
        }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Timed out waiting for simulation API readiness; inspect $stderrPath."
}

function Stop-SimulationAPI {
    if (-not (Test-Path -LiteralPath $apiStatePath -PathType Leaf)) { Write-Output 'No recorded simulation API process to stop.'; return }
    $state = Get-Content -LiteralPath $apiStatePath -Raw | ConvertFrom-Json
    if ([IO.Path]::GetFullPath([string]$state.binary) -ne [IO.Path]::GetFullPath($binaryPath)) {
        throw 'API process record points to a different executable; refusing to stop it.'
    }
    $process = Get-Process -Id ([int]$state.pid) -ErrorAction SilentlyContinue
    if ($null -ne $process) {
        $actualPath = ''
        $actualTicks = 0L
        $recordedTicks = 0L
        try { $actualPath = [IO.Path]::GetFullPath($process.Path) } catch { }
        try { $actualTicks = $process.StartTime.ToUniversalTime().Ticks } catch { }
        try { $recordedTicks = ([DateTime]$state.started_utc).ToUniversalTime().Ticks } catch { }
        if ($actualPath -ne [IO.Path]::GetFullPath($binaryPath) -or $actualTicks -ne $recordedTicks) {
            throw "PID $($state.pid) no longer identifies this simulation API; refusing to stop it."
        }
        Stop-Process -Id $process.Id
    }
    Remove-Item -LiteralPath $apiStatePath
    Write-Output 'Stopped only the recorded isolated simulation API process.'
}

function Stop-SimulationMySQL {
    $state = Get-ContainerState
    if ($null -eq $state) { Write-Output 'No isolated shopping MySQL container exists.'; return }
    if ($state.Labels.'com.stablepay.simulation' -ne 'agent-shopping') {
        throw "Container name $containerName is not labeled as this simulation; refusing to stop it."
    }
    if ($state.Status -eq 'running') { Invoke-Docker @('stop', $containerName) | Out-Null }
    Write-Output "Stopped $containerName; its ignored .local-run data directory was preserved."
}

if (-not ($StartMySQL -or $StartMerchants -or $Seed -or $StartAPI -or $StopAPI -or $StopMySQL -or $StopMerchants -or $Status)) {
    Write-Output 'Use -Seed for isolated MySQL + simulated Merchant import, -StartAPI -UseMerchantHTTPSource to serve live Merchant HTTP catalog data, and -StopAPI/-StopMySQL/-StopMerchants for exact-target shutdown.'
    exit 0
}
if ($UseMerchantHTTPSource -and -not $StartAPI) { throw '-UseMerchantHTTPSource requires -StartAPI.' }

if ($StartMySQL) { Ensure-MySQL; Write-Output 'Isolated MySQL is healthy at 127.0.0.1:13327/stablepay_shopping_sim' }
if ($StartMerchants) { Ensure-Merchants }
if ($Seed) { Invoke-Seed }
if ($StartAPI) { Start-SimulationAPI; Write-Output "Simulation API is ready at http://$apiAddr" }
if ($StopAPI) { Stop-SimulationAPI }
if ($StopMySQL) { Stop-SimulationMySQL }
if ($StopMerchants) { & powershell -NoProfile -ExecutionPolicy Bypass -File $merchantScript -Stop }
if ($Status) {
    $mysqlState = Get-ContainerState
    $apiHealthy = $false
    try { $apiHealthy = (Invoke-RestMethod -Uri 'http://127.0.0.1:18090/readyz' -TimeoutSec 2).status -eq 'ready' } catch { }
    [pscustomobject]@{ mysql_container = if ($null -eq $mysqlState) { 'absent' } else { $mysqlState.Status }; mysql_health = if ($null -eq $mysqlState) { 'absent' } else { $mysqlState.Health }; merchant_health_count = Test-MerchantHealth; api_ready = $apiHealthy } | ConvertTo-Json -Compress
}
