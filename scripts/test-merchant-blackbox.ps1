$ErrorActionPreference = 'Stop'

$repoRoot = (Get-Location).Path
$merchantRoot = Join-Path $repoRoot 'merchant'
$runtimeRoot = Join-Path $repoRoot 'commerce-runtime'
$runRoot = Join-Path $repoRoot ('.local-run\merchant-blackbox-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff'))
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null

$env:GOCACHE = Join-Path $runRoot 'go-cache'
$env:GOMODCACHE = Join-Path $runRoot 'go-mod-cache'
$env:SERVER_HOST = '127.0.0.1'
$env:SERVER_PORT = '8787'
$env:DB_DRIVER = 'sqlite'
$env:DB_PATH = Join-Path $runRoot 'merchant.db'
$env:DB_AUTO_MIGRATE = 'true'
$env:DB_SEED_ENABLED = 'true'
$env:MERCHANT_PUBLIC_BASE_URL = 'http://127.0.0.1:8787'
$env:MERCHANT_SELLER_ADDRESS = '2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR'
$env:MERCHANT_PROOF_SECRET = 'blackbox-test-secret'
$env:SOLANA_NETWORK = 'solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp'
$env:USDC_MINT = 'EPjFWdd5AufQSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v'

$env:MERCHANT_TEST_MODE = 'true'

$serverScript = {
    $env:GOCACHE = Join-Path $using:runRoot 'go-cache'
    $env:GOMODCACHE = Join-Path $using:runRoot 'go-mod-cache'
    $env:SERVER_HOST = '127.0.0.1'
    $env:SERVER_PORT = '8787'
    $env:DB_DRIVER = 'sqlite'
    $env:DB_PATH = Join-Path $using:runRoot 'merchant.db'
    $env:DB_AUTO_MIGRATE = 'true'
    $env:DB_SEED_ENABLED = 'true'
    $env:MERCHANT_PUBLIC_BASE_URL = 'http://127.0.0.1:8787'
    $env:MERCHANT_SELLER_ADDRESS = '2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR'
    $env:MERCHANT_PROOF_SECRET = 'blackbox-test-secret'
    $env:SOLANA_NETWORK = 'solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp'
    $env:USDC_MINT = 'EPjFWdd5AufQSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v'
    $env:MERCHANT_TEST_MODE = 'true'
    Set-Location $using:merchantRoot
    & go run ./cmd/merchant-server
}

function Wait-MerchantReady {
    $healthy = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        Start-Sleep -Milliseconds 250
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8787/healthz' -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                $healthy = $true
                break
            }
        } catch {
            if ($serverJob.State -in @('Failed', 'Stopped', 'Completed')) {
                throw "merchant-server job ended before readiness: $(Receive-Job -Id $serverJob.Id -Keep | Out-String)"
            }
        }
    }
    if (-not $healthy) {
        throw "merchant-server did not become ready"
    }
}

$serverJob = Start-Job -ScriptBlock $serverScript

try {
    $env:MERCHANT_BLACKBOX_ENDPOINT = 'http://127.0.0.1:8787/api/v1/products/ai-agent-job-2025/execute'
    $env:MERCHANT_BLACKBOX_EXPECTED_PATH = Join-Path $runRoot 'paid-response.json'
    $env:MERCHANT_BLACKBOX_RESTART_REPLAY = 'false'
    Wait-MerchantReady
    Set-Location $runtimeRoot
    & go run ./cmd/merchant-blackbox
    if ($LASTEXITCODE -ne 0) {
        throw "commerce-runtime merchant black-box command failed"
    }

    Stop-Job -Id $serverJob.Id -ErrorAction SilentlyContinue
    Remove-Job -Id $serverJob.Id -ErrorAction SilentlyContinue
    $serverJob = Start-Job -ScriptBlock $serverScript
    $env:MERCHANT_BLACKBOX_RESTART_REPLAY = 'true'
    Wait-MerchantReady
    & go run ./cmd/merchant-blackbox
    if ($LASTEXITCODE -ne 0) {
        throw "commerce-runtime merchant restart black-box command failed"
    }
    Write-Output "merchant actual-server black-box passed; logs and SQLite state: $runRoot"
} finally {
    if ($serverJob) {
        Stop-Job -Id $serverJob.Id -ErrorAction SilentlyContinue
        Remove-Job -Id $serverJob.Id -ErrorAction SilentlyContinue
    }
}
