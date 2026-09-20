$ErrorActionPreference = 'Stop'

$repoRoot = (Get-Location).Path
$merchantRoot = Join-Path $repoRoot 'merchant'
$runtimeRoot = Join-Path $repoRoot 'commerce-runtime'
$runRoot = Join-Path $repoRoot ('.local-run\business-e2e-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff'))
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
$goCache = Join-Path $repoRoot '.local-run\go-cache-business-e2e'
$goModCache = Join-Path $repoRoot '.local-run\go-mod-cache-business-e2e'

function Import-EnvFile([string] $path) {
    if (-not (Test-Path -LiteralPath $path)) { return }
    foreach ($line in Get-Content -LiteralPath $path) {
        if ($line -match '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') {
            $name = $Matches[1]
            $value = $Matches[2].Trim()
            if (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'"))) {
                $value = $value.Substring(1, $value.Length - 2)
            }
            if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
                [Environment]::SetEnvironmentVariable($name, $value, 'Process')
            }
        }
    }
}

Import-EnvFile (Join-Path $repoRoot '.env')

# The real-provider acceptance path is intentionally OpenAI-compatible and
# uses the user's configured endpoint. It never selects GLM implicitly.
$env:LLM_PROVIDER = 'openai-compatible'
if ([string]::IsNullOrWhiteSpace($env:LLM_BASE_URL) -or [string]::IsNullOrWhiteSpace($env:LLM_API_KEY) -or [string]::IsNullOrWhiteSpace($env:LLM_MODEL_ID)) {
    throw 'LLM_BASE_URL, LLM_API_KEY, and LLM_MODEL_ID must be set in the process or .env'
}
$env:LLM_MODEL = $env:LLM_MODEL_ID

$env:GOCACHE = $goCache
$env:GOMODCACHE = $goModCache
$env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK = if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK)) { 'solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp' } else { $env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK }
$env:COMMERCE_RUNTIME_USDC_MINT = if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_USDC_MINT)) { 'EPjFWdd5AufQSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v' } else { $env:COMMERCE_RUNTIME_USDC_MINT }
$env:SOLANA_NETWORK = $env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK
$env:USDC_MINT = $env:COMMERCE_RUNTIME_USDC_MINT

if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_MYSQL_DSN)) {
    throw 'COMMERCE_RUNTIME_MYSQL_DSN must point at the actual MySQL S1-S6 test database'
}

$merchantPort = '8788'
$merchantBaseURL = "http://127.0.0.1:$merchantPort"
$merchantEndpoint = "http://127.0.0.1:$merchantPort/api/v1/products/ai-agent-job-2025/execute"
$merchantDB = Join-Path $runRoot 'merchant.db'
$reportPath = Join-Path $runRoot 'business-e2e-summary.json'
$sellerAddress = $env:MERCHANT_SELLER_ADDRESS
if ([string]::IsNullOrWhiteSpace($sellerAddress)) {
    $sellerAddress = '2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZKJWdR'
    $env:MERCHANT_SELLER_ADDRESS = $sellerAddress
}
$settlementNetwork = $env:SOLANA_NETWORK
$settlementMint = $env:USDC_MINT
$merchantServerScript = {
    $env:GOCACHE = $using:goCache
    $env:GOMODCACHE = $using:goModCache
    $env:SERVER_HOST = '127.0.0.1'
    $env:SERVER_PORT = $using:merchantPort
    $env:DB_DRIVER = 'sqlite'
    $env:DB_PATH = $using:merchantDB
    $env:DB_AUTO_MIGRATE = 'true'
    $env:DB_SEED_ENABLED = 'true'
    $env:MERCHANT_PUBLIC_BASE_URL = $using:merchantBaseURL
    $env:MERCHANT_SELLER_ADDRESS = $using:sellerAddress
    $env:MERCHANT_PROOF_SECRET = 'business-e2e-test-secret'
    $env:SOLANA_NETWORK = $using:settlementNetwork
    $env:USDC_MINT = $using:settlementMint
    $env:MERCHANT_TEST_MODE = 'true'
    Set-Location $using:merchantRoot
    & go run ./cmd/merchant-server
}

function Wait-MerchantReady($job) {
    for ($attempt = 0; $attempt -lt 80; $attempt++) {
        Start-Sleep -Milliseconds 250
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$merchantPort/healthz" -TimeoutSec 2
            if ($response.StatusCode -eq 200) { return }
        } catch {
            if ($job.State -in @('Failed', 'Stopped', 'Completed')) {
                throw "merchant-server ended before readiness: $(Receive-Job -Id $job.Id -Keep | Out-String)"
            }
        }
    }
    throw 'merchant-server did not become ready'
}

$serverJob = Start-Job -ScriptBlock $merchantServerScript
try {
    Wait-MerchantReady $serverJob
    $env:MERCHANT_E2E_ENDPOINT = $merchantEndpoint
    $env:MERCHANT_SELLER_ADDRESS = $sellerAddress
    $env:BUSINESS_E2E_REPORT_PATH = $reportPath
    Set-Location $runtimeRoot
    & go run ./cmd/business-e2e
    if ($LASTEXITCODE -ne 0) { throw 'business-e2e harness failed' }
    Write-Output "business E2E passed; actual Merchant SQLite and JSON report: $runRoot"
} finally {
    if ($serverJob) {
        Stop-Job -Id $serverJob.Id -ErrorAction SilentlyContinue
        Remove-Job -Id $serverJob.Id -ErrorAction SilentlyContinue
    }
}
