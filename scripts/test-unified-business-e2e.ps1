[CmdletBinding()]
param(
    [ValidateSet('U1', 'U2')]
    [string]$Mode = 'U1',
    [switch]$SkipUnitGate,
    [switch]$SkipStart,
    [switch]$KeepServices,
    [switch]$KeepInfra,
    [int]$TimeoutSeconds = 180,
    [string]$WalletPath = $env:STABLEPAY_HOTWALLET_PATH,
    [string]$AgentKeypairPath = $env:STABLEPAY_E2E_AGENT_KEYPAIR_PATH
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$infraDir = Join-Path $root 'stablepayai-idl'
$composeFile = Join-Path $infraDir 'docker-compose.infra.yml'
$startScript = Join-Path $PSScriptRoot 'start-local.ps1'
$stopScript = Join-Path $PSScriptRoot 'stop-local.ps1'
$unitScript = Join-Path $PSScriptRoot 'test-six-services.ps1'
$merchantRoot = Join-Path $root 'merchant'
$runtimeRoot = Join-Path $root 'commerce-runtime'
$paymentRoot = Join-Path $root 'payment-service'
$defaultWallet = Join-Path $root 'blockchain-adapter\config\hotwallet.json'
$defaultAgent = Join-Path $root '.local-run\secrets\e2e-agent.json'
$runRoot = Join-Path $root ('.local-run\unified-business-e2e-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff'))
$preparedPath = Join-Path $runRoot 'prepared.json'
$reportPath = Join-Path $runRoot 'unified-business-e2e-summary.json'
$merchantDB = Join-Path $runRoot 'merchant.db'
$merchantDBB = Join-Path $runRoot 'merchant-b.db'
$merchantPort = 8788
$merchantPortB = 8789
$merchantBaseURL = "http://127.0.0.1:$merchantPort"
$merchantBaseURLB = "http://127.0.0.1:$merchantPortB"
$merchantEndpoint = "$merchantBaseURL/api/v1/products/ai-agent-job-2025/execute"
$merchantEndpointB = "$merchantBaseURLB/api/v1/products/ai-agent-job-2025/execute"
$verificationDatabase = 'stablepay_verification_unified_' + ([guid]::NewGuid().ToString('N'))
$verificationGroup = 'verification_unified_' + ([guid]::NewGuid().ToString('N'))

function Import-EnvFile([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return }
    foreach ($line in Get-Content -LiteralPath $Path) {
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

Import-EnvFile (Join-Path $root '.env')
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null
$env:GOCACHE = Join-Path $root '.local-run\gocache-l2'
$env:GOMODCACHE = Join-Path $root '.local-run\gomodcache-l2'
$env:GOSUMDB = 'off'
$env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK = if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK)) { 'solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp' } else { $env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK }
$env:COMMERCE_RUNTIME_USDC_MINT = if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_USDC_MINT)) { 'EPjFWdd5AufQSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v' } else { $env:COMMERCE_RUNTIME_USDC_MINT }
$env:SOLANA_NETWORK = $env:COMMERCE_RUNTIME_SETTLEMENT_NETWORK
$env:USDC_MINT = $env:COMMERCE_RUNTIME_USDC_MINT
if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_MYSQL_DSN)) { $env:COMMERCE_RUNTIME_MYSQL_DSN = 'stablepay:stablepay123@tcp(127.0.0.1:3307)/stablepay_payment_db?parseTime=true' }
if ([string]::IsNullOrWhiteSpace($WalletPath)) { $WalletPath = $defaultWallet }
if (-not [IO.Path]::IsPathRooted($WalletPath)) { $WalletPath = Join-Path $root $WalletPath }
$WalletPath = [IO.Path]::GetFullPath($WalletPath)
if ([string]::IsNullOrWhiteSpace($AgentKeypairPath)) { $AgentKeypairPath = $defaultAgent }
if (-not [IO.Path]::IsPathRooted($AgentKeypairPath)) { $AgentKeypairPath = Join-Path $root $AgentKeypairPath }
$AgentKeypairPath = [IO.Path]::GetFullPath($AgentKeypairPath)
$env:STABLEPAY_HOTWALLET_PATH = $WalletPath
$env:STABLEPAY_E2E_AGENT_KEYPAIR_PATH = $AgentKeypairPath
$env:STABLEPAY_DID_SERVICE_ADDR = if ([string]::IsNullOrWhiteSpace($env:STABLEPAY_DID_SERVICE_ADDR)) { '127.0.0.1:8081' } else { $env:STABLEPAY_DID_SERVICE_ADDR }
$env:STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR = if ([string]::IsNullOrWhiteSpace($env:STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR)) { '127.0.0.1:8083' } else { $env:STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR }
$env:STABLEPAY_PAYMENT_SERVICE_ADDR = if ([string]::IsNullOrWhiteSpace($env:STABLEPAY_PAYMENT_SERVICE_ADDR)) { '127.0.0.1:8888' } else { $env:STABLEPAY_PAYMENT_SERVICE_ADDR }
$env:STABLEPAY_GATEWAY_BASE_URL = if ([string]::IsNullOrWhiteSpace($env:STABLEPAY_GATEWAY_BASE_URL)) { 'http://127.0.0.1:8080' } else { $env:STABLEPAY_GATEWAY_BASE_URL }
$env:STABLEPAY_API_KEY = if ([string]::IsNullOrWhiteSpace($env:STABLEPAY_API_KEY)) { 'stablepay-dev-key' } else { $env:STABLEPAY_API_KEY }
$goCache = $env:GOCACHE
$goModCache = $env:GOMODCACHE
$gatewayURL = $env:STABLEPAY_GATEWAY_BASE_URL
$apiKey = $env:STABLEPAY_API_KEY
$settlementNetwork = $env:SOLANA_NETWORK
$settlementMint = $env:USDC_MINT
if ($Mode -eq 'U2') {
    if ([string]::IsNullOrWhiteSpace($env:LLM_BASE_URL) -or [string]::IsNullOrWhiteSpace($env:LLM_API_KEY) -or [string]::IsNullOrWhiteSpace($env:LLM_MODEL_ID)) {
        throw 'U2 requires LLM_BASE_URL, LLM_API_KEY and LLM_MODEL_ID in the process or .env'
    }
    $env:LLM_PROVIDER = 'openai-compatible'
    $env:LLM_MODEL = $env:LLM_MODEL_ID
}

function Fail([string]$Message) { throw "[unified-business-e2e] $Message" }

$dockerCommand = Get-Command docker.exe -ErrorAction SilentlyContinue
$dockerExe = if ($dockerCommand) { $dockerCommand.Source } else {
    @(
        if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources\bin\docker.exe' }
        if ($env:ProgramFiles) { Join-Path $env:ProgramFiles 'Docker\Docker\resources\bin\docker.exe' }
    ) | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
}
if (-not $dockerExe) { Fail 'docker.exe is required; fake-chain is not used' }

function Wait-Tcp([int]$Port, [string]$Name, [int]$Seconds = 90) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    do {
        if (Test-NetConnection -ComputerName 127.0.0.1 -Port $Port -InformationLevel Quiet -WarningAction SilentlyContinue) { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    Fail "$Name did not listen on 127.0.0.1:$Port"
}

function Wait-Merchant($Job, [string]$BaseURL) {
    for ($attempt = 0; $attempt -lt 100; $attempt++) {
        Start-Sleep -Milliseconds 300
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "$BaseURL/healthz" -TimeoutSec 2
            if ($response.StatusCode -eq 200) { return }
        } catch {
            if ($Job.State -in @('Failed', 'Stopped', 'Completed')) { Fail "production Merchant exited before readiness: $(Receive-Job -Id $Job.Id -Keep | Out-String)" }
        }
    }
    Fail 'production Merchant did not become ready'
}

$infraStarted = $false
$servicesStarted = $false
$merchantJob = $null
$merchantJobB = $null
$previousVerificationGroup = $env:VERIFICATION_ROCKETMQ_GROUP
$previousVerificationDSN = $env:VERIFICATION_MYSQL_DSN
$env:VERIFICATION_ROCKETMQ_GROUP = $verificationGroup
try {
    Write-Host '[unified-business-e2e] L2/U1 real infrastructure + production Merchant + real payment/verification'
    if (-not $SkipUnitGate) {
        & powershell -NoProfile -ExecutionPolicy Bypass -File $unitScript
        if ($LASTEXITCODE -ne 0) { Fail 'unit/contract gate failed' }
    }
    if (-not $SkipStart) {
        Push-Location $infraDir
        try {
            & $dockerExe compose -f $composeFile up -d
            if ($LASTEXITCODE -ne 0) { Fail 'failed to start MySQL/Redis/RocketMQ' }
            $infraStarted = $true
        } finally { Pop-Location }
        Wait-Tcp 3307 'MySQL'
        Wait-Tcp 6379 'Redis'
        Wait-Tcp 9876 'RocketMQ nameserver'
        Wait-Tcp 10911 'RocketMQ broker'

        $databaseSQL = "CREATE DATABASE IF NOT EXISTS $verificationDatabase; GRANT ALL PRIVILEGES ON $verificationDatabase.* TO 'stablepay'@'%'; FLUSH PRIVILEGES;"
        $databaseReady = $false
        for ($attempt = 1; $attempt -le 30; $attempt++) {
            $oldEAP = $ErrorActionPreference
            $ErrorActionPreference = 'Continue'
            try { $databaseOutput = & $dockerExe exec -e MYSQL_PWD=root123 stablepay-mysql mysql --protocol=TCP -h 127.0.0.1 -uroot -e $databaseSQL 2>&1; $databaseExit = $LASTEXITCODE } finally { $ErrorActionPreference = $oldEAP }
            if ($databaseExit -eq 0) { $databaseReady = $true; break }
            Start-Sleep -Seconds 2
        }
        if (-not $databaseReady) { Fail "could not create isolated verification database: $($databaseOutput -join ' ')" }
        # A Docker-published MySQL port can accept TCP probes before the
        # host-side listener is ready for external services. Probe the same
        # host:port used by the Go services before starting them.
        $mysqlExternalReady = $false
        for ($attempt = 1; $attempt -le 30; $attempt++) {
            $oldEAP = $ErrorActionPreference
            $ErrorActionPreference = 'Continue'
            try { $mysqlProbe = & $dockerExe exec -e MYSQL_PWD=stablepay123 stablepay-mysql mysql --protocol=TCP -h host.docker.internal -P 3307 -ustablepay -e 'SELECT 1' 2>&1; $mysqlProbeExit = $LASTEXITCODE } finally { $ErrorActionPreference = $oldEAP }
            if ($mysqlProbeExit -eq 0) { $mysqlExternalReady = $true; break }
            Start-Sleep -Seconds 2
        }
        if (-not $mysqlExternalReady) { Fail "MySQL host port 3307 was not usable by application services: $($mysqlProbe -join ' ')" }
        $env:VERIFICATION_MYSQL_DSN = "stablepay:stablepay123@tcp(127.0.0.1:3307)/${verificationDatabase}?charset=utf8mb4&parseTime=True&loc=Local"
        Write-Host "[unified-business-e2e] verification database isolated"

        $topicReady = $false
        for ($attempt = 1; $attempt -le 30; $attempt++) {
            $topicOutput = & $dockerExe exec stablepay-rocketmq-broker sh -c 'sh /home/rocketmq/rocketmq-5.3.2/bin/mqadmin updateTopic -n rocketmq-nameserver:9876 -c DefaultCluster -t payment_events' 2>&1
            if ($LASTEXITCODE -eq 0 -and ($topicOutput -join "`n") -match 'create topic .* success') { $topicReady = $true; break }
            Start-Sleep -Seconds 2
        }
        if (-not $topicReady) { Fail 'could not create RocketMQ payment_events topic' }
        & powershell -NoProfile -ExecutionPolicy Bypass -File $startScript -WalletPath $WalletPath
        if ($LASTEXITCODE -ne 0) { Fail 'six real services did not start' }
        $servicesStarted = $true
    }

    Wait-Tcp 8081 'did-service'
    Wait-Tcp 8083 'blockchain-adapter'
    Wait-Tcp 8084 'query-service'
    Wait-Tcp 8085 'verification-service'
    Wait-Tcp 8888 'payment-service'
    Wait-Tcp 8080 'api-gateway'
    $verificationLog = Join-Path $root '.local-run\logs\verification-service.out.log'
    $readyDeadline = (Get-Date).AddSeconds(90)
    do {
        if (Test-Path -LiteralPath $verificationLog) {
            $readyLine = Get-Content -LiteralPath $verificationLog -Tail 100 | Select-String 'RocketMQ consumer ready'
            if ($readyLine) { break }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $readyDeadline)
    if (-not $readyLine) { Fail 'verification-service readiness was not observed' }

    # This helper only registers/checks the real identities and builds an
    # unsigned/partial transaction. It does not submit a payment. U1's
    # CredentialProvider builds the exact transaction for its persisted intent.
    $helperArgs = @('run', './cmd/e2e-client', '-agent-keypair-path', $AgentKeypairPath, '-hot-wallet-path', $WalletPath, '-amount', '0.01', '-currency', 'USDC', '-output', $preparedPath)
    Push-Location $paymentRoot
    try {
        $oldGoEAP = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        try { $helperOutput = & go @helperArgs 2>&1 } finally { $ErrorActionPreference = $oldGoEAP }
        if ($LASTEXITCODE -ne 0) { Fail "real identity preflight failed: $($helperOutput -join ' ')" }
    } finally { Pop-Location }
    $prepared = Get-Content -Raw -LiteralPath $preparedPath | ConvertFrom-Json
    $sellerAddress = [string]$prepared.skill_public_key
    $skillDID = [string]$prepared.skill_did
    if ([string]::IsNullOrWhiteSpace($sellerAddress) -or [string]::IsNullOrWhiteSpace($skillDID)) { Fail 'real identity preflight did not return the Skill DID/wallet' }
    $agentKeypair = Get-Content -Raw -LiteralPath $AgentKeypairPath | ConvertFrom-Json
    $agentAddress = [string]$agentKeypair.public_key
    if ([string]::IsNullOrWhiteSpace($agentAddress)) { $agentAddress = [string]$agentKeypair.address }
    if ([string]::IsNullOrWhiteSpace($agentAddress)) { Fail 'real Agent keypair did not expose a public key/address' }
    # U2 needs two real verification identities. The local preflight proves
    # this Agent wallet owns the canonical Devnet token account, so it is a
    # deterministic second payee when no explicit B seller is supplied.
    $sellerAddressB = if ([string]::IsNullOrWhiteSpace($env:MERCHANT_B_SELLER_ADDRESS)) { $agentAddress } else { $env:MERCHANT_B_SELLER_ADDRESS }
    $skillDIDB = if ([string]::IsNullOrWhiteSpace($env:MERCHANT_E2E_PAYEE_DID_B)) { "did:solana:$sellerAddressB" } else { $env:MERCHANT_E2E_PAYEE_DID_B }

    $merchantScript = {
        $env:GOCACHE = $using:goCache
        $env:GOMODCACHE = $using:goModCache
        $env:GOSUMDB = 'off'
        $env:SERVER_HOST = '127.0.0.1'
        $env:SERVER_PORT = [string]$using:merchantPort
        $env:DB_DRIVER = 'sqlite'
        $env:DB_PATH = $using:merchantDB
        $env:DB_AUTO_MIGRATE = 'true'
        $env:DB_SEED_ENABLED = 'true'
        $env:MERCHANT_PUBLIC_BASE_URL = $using:merchantBaseURL
        $env:MERCHANT_SELLER_ADDRESS = $using:sellerAddress
        $env:MERCHANT_PROOF_SECRET = 'unified-business-e2e-production-secret'
        $env:STABLEPAY_GATEWAY_BASE_URL = $using:gatewayURL
        $env:STABLEPAY_API_KEY = $using:apiKey
        $env:STABLEPAY_FACILITATOR_URL = $using:gatewayURL
        $env:SOLANA_NETWORK = $using:settlementNetwork
        $env:USDC_MINT = $using:settlementMint
        $env:MERCHANT_TEST_MODE = 'false'
        Set-Location -LiteralPath $using:merchantRoot
        & go run ./cmd/merchant-server
    }
    $merchantJob = Start-Job -ScriptBlock $merchantScript
    Wait-Merchant $merchantJob $merchantBaseURL

    if ($Mode -eq 'U2') {
        $merchantScriptB = {
            $env:GOCACHE = $using:goCache
            $env:GOMODCACHE = $using:goModCache
            $env:GOSUMDB = 'off'
            $env:SERVER_HOST = '127.0.0.1'
            $env:SERVER_PORT = [string]$using:merchantPortB
            $env:DB_DRIVER = 'sqlite'
            $env:DB_PATH = $using:merchantDBB
            $env:DB_AUTO_MIGRATE = 'true'
            $env:DB_SEED_ENABLED = 'true'
            $env:MERCHANT_PUBLIC_BASE_URL = $using:merchantBaseURLB
            $env:MERCHANT_SELLER_ADDRESS = $using:sellerAddressB
            $env:MERCHANT_PROOF_SECRET = 'unified-business-e2e-production-secret-b'
            $env:STABLEPAY_GATEWAY_BASE_URL = $using:gatewayURL
            $env:STABLEPAY_API_KEY = $using:apiKey
            $env:STABLEPAY_FACILITATOR_URL = $using:gatewayURL
            $env:SOLANA_NETWORK = $using:settlementNetwork
            $env:USDC_MINT = $using:settlementMint
            $env:MERCHANT_TEST_MODE = 'false'
            Set-Location -LiteralPath $using:merchantRoot
            & go run ./cmd/merchant-server
        }
        $merchantJobB = Start-Job -ScriptBlock $merchantScriptB
        Wait-Merchant $merchantJobB $merchantBaseURLB
    }

    $env:MERCHANT_E2E_ENDPOINT = $merchantEndpoint
    $env:MERCHANT_E2E_PAYEE_DID = $skillDID
    $env:MERCHANT_E2E_ENDPOINT_A = $merchantEndpoint
    $env:MERCHANT_E2E_ENDPOINT_B = $merchantEndpointB
    $env:MERCHANT_E2E_PAYEE_DID_A = $skillDID
    $env:MERCHANT_E2E_PAYEE_DID_B = $skillDIDB
    $env:MERCHANT_SELLER_ADDRESS = $sellerAddress
    $env:MERCHANT_B_SELLER_ADDRESS = $sellerAddressB
    $env:UNIFIED_BUSINESS_E2E_REPORT_PATH = $reportPath
    $env:UNIFIED_BUSINESS_E2E_MODE = $Mode
    Push-Location $runtimeRoot
    try {
        $oldGoEAP = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        try { & go run ./cmd/unified-business-e2e } finally { $ErrorActionPreference = $oldGoEAP }
        if ($LASTEXITCODE -ne 0) { Fail 'unified U1 Go harness failed' }
    } finally { Pop-Location }
    Write-Host "[unified-business-e2e] PASS; report=$reportPath"
} finally {
    if ($merchantJob) { Stop-Job -Id $merchantJob.Id -ErrorAction SilentlyContinue; Remove-Job -Id $merchantJob.Id -Force -ErrorAction SilentlyContinue }
    if ($merchantJobB) { Stop-Job -Id $merchantJobB.Id -ErrorAction SilentlyContinue; Remove-Job -Id $merchantJobB.Id -Force -ErrorAction SilentlyContinue }
    if ($null -eq $previousVerificationGroup) { Remove-Item Env:VERIFICATION_ROCKETMQ_GROUP -ErrorAction SilentlyContinue } else { $env:VERIFICATION_ROCKETMQ_GROUP = $previousVerificationGroup }
    if ($null -eq $previousVerificationDSN) { Remove-Item Env:VERIFICATION_MYSQL_DSN -ErrorAction SilentlyContinue } else { $env:VERIFICATION_MYSQL_DSN = $previousVerificationDSN }
    if (-not $KeepServices -and $servicesStarted) { & powershell -NoProfile -ExecutionPolicy Bypass -File $stopScript }
    if (-not $KeepInfra -and $infraStarted) { Push-Location $infraDir; try { & $dockerExe compose -f $composeFile down } finally { Pop-Location } }
}
