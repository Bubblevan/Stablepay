[CmdletBinding()]
param(
    [switch]$SkipUnitGate,
    [switch]$SkipStart,
    [switch]$KeepServices,
    [switch]$KeepInfra,
    [string]$WalletPath = $env:STABLEPAY_HOTWALLET_PATH,
    [string]$AgentDID = $env:STABLEPAY_E2E_AGENT_DID,
    [string]$SkillDID = $env:STABLEPAY_E2E_SKILL_DID,
    [string]$PaymentSignature = $env:STABLEPAY_E2E_PAYMENT_SIGNATURE,
    [string]$GatewaySignature = $env:STABLEPAY_E2E_GATEWAY_SIGNATURE,
    [string]$Timestamp = $env:STABLEPAY_E2E_TIMESTAMP,
    [string]$Nonce = $env:STABLEPAY_E2E_NONCE,
    [string]$SignedTxBase64 = $env:STABLEPAY_E2E_SIGNED_TX_BASE64,
    [string]$IdempotencyKey = $env:STABLEPAY_E2E_IDEMPOTENCY_KEY,
    [string]$Amount = $(if ($env:STABLEPAY_E2E_AMOUNT) { $env:STABLEPAY_E2E_AMOUNT } else { "0.01" }),
    [string]$Currency = $(if ($env:STABLEPAY_E2E_CURRENCY) { $env:STABLEPAY_E2E_CURRENCY } else { "USDC" }),
    [int]$TimeoutSeconds = 120
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$infraDir = Join-Path $root "stablepayai-idl"
$composeFile = Join-Path $infraDir "docker-compose.infra.yml"
$unitScript = Join-Path $PSScriptRoot "test-six-services.ps1"
$startScript = Join-Path $PSScriptRoot "start-local.ps1"
$stopScript = Join-Path $PSScriptRoot "stop-local.ps1"
$replayDir = Join-Path $root "payment-service"
$payloadFile = Join-Path $root ".local-run\e2e-payment-event.json"

function Fail([string]$Message) { throw "[deterministic-e2e] $Message" }

$dockerExe = $null
$dockerCommand = Get-Command docker.exe -ErrorAction SilentlyContinue
if ($dockerCommand) {
    $dockerExe = $dockerCommand.Source
} else {
    $dockerCandidates = @()
    if ($env:LOCALAPPDATA) {
        $dockerCandidates += Join-Path $env:LOCALAPPDATA "Programs\DockerDesktop\resources\bin\docker.exe"
    }
    if ($env:ProgramFiles) {
        $dockerCandidates += Join-Path $env:ProgramFiles "Docker\Docker\resources\bin\docker.exe"
    }
    $dockerExe = $dockerCandidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
}

function Wait-Tcp([int]$Port, [string]$Name, [int]$Seconds = 60) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    do {
        if (Test-NetConnection -ComputerName 127.0.0.1 -Port $Port -InformationLevel Quiet -WarningAction SilentlyContinue) { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    Fail "$Name did not listen on 127.0.0.1:$Port"
}

function Invoke-Json([string]$Method, [string]$Uri, [hashtable]$Headers, [object]$Body = $null) {
    $params = @{ Method = $Method; Uri = $Uri; Headers = $Headers; TimeoutSec = $TimeoutSeconds }
    if ($null -ne $Body) {
        $params.ContentType = "application/json"
        $params.Body = ($Body | ConvertTo-Json -Depth 8 -Compress)
    }
    return Invoke-RestMethod @params
}

Write-Host "[deterministic-e2e] real infrastructure + real Solana Devnet path"

if (-not $SkipUnitGate) {
    & powershell -NoProfile -ExecutionPolicy Bypass -File $unitScript
    if ($LASTEXITCODE -ne 0) { Fail "unit/contract gate failed" }
}

if (-not $dockerExe) { Fail "docker is required; fake-chain is intentionally not used" }
if (-not (Test-Path -LiteralPath $composeFile -PathType Leaf)) { Fail "infra compose file missing: $composeFile" }

$infraStarted = $false
$servicesStarted = $false
try {
    Push-Location $infraDir
    try {
        & $dockerExe compose -f $composeFile up -d
        if ($LASTEXITCODE -ne 0) { Fail "failed to start MySQL/Redis/RocketMQ compose" }
        $infraStarted = $true
    } finally { Pop-Location }

    Wait-Tcp 3307 "MySQL"
    Wait-Tcp 6379 "Redis"
    Wait-Tcp 9876 "RocketMQ nameserver"
    Wait-Tcp 10911 "RocketMQ broker"

    $topicReady = $false
    for ($attempt = 1; $attempt -le 30; $attempt++) {
        & $dockerExe exec stablepay-rocketmq-broker sh -c "sh /home/rocketmq/rocketmq-5.3.2/bin/mqadmin updateTopic -n rocketmq-nameserver:9876 -c DefaultCluster -t payment_events"
        if ($LASTEXITCODE -eq 0) { $topicReady = $true; break }
        Start-Sleep -Seconds 2
    }
    if (-not $topicReady) { Fail "could not create/check RocketMQ physical topic payment_events" }

    if (-not $SkipStart) {
        if ([string]::IsNullOrWhiteSpace($WalletPath)) { Fail "WalletPath is required for real Devnet E2E" }
        & powershell -NoProfile -ExecutionPolicy Bypass -File $startScript -WalletPath $WalletPath
        if ($LASTEXITCODE -ne 0) { Fail "local services did not start" }
        $servicesStarted = $true
    }

    Wait-Tcp 8081 "did-service"
    Wait-Tcp 8083 "blockchain-adapter"
    Wait-Tcp 8084 "query-service"
    Wait-Tcp 8085 "verification-service Kitex"
    Wait-Tcp 8888 "payment-service"
    Wait-Tcp 8080 "api-gateway"

    $verificationLog = Join-Path $root ".local-run\logs\verification-service.out.log"
    $readyDeadline = (Get-Date).AddSeconds(60)
    do {
        if (Test-Path -LiteralPath $verificationLog) {
            $readyLine = Get-Content -LiteralPath $verificationLog -Tail 80 | Select-String "RocketMQ consumer ready"
            if ($readyLine) { break }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $readyDeadline)
    if (-not $readyLine) { Fail "verification-service Kitex port is open but RocketMQ consumer readiness was not observed" }

    $required = @{
        AgentDID = $AgentDID; SkillDID = $SkillDID; PaymentSignature = $PaymentSignature;
        GatewaySignature = $GatewaySignature; Timestamp = $Timestamp; Nonce = $Nonce;
        SignedTxBase64 = $SignedTxBase64; IdempotencyKey = $IdempotencyKey
    }
    foreach ($entry in $required.GetEnumerator()) {
        if ([string]::IsNullOrWhiteSpace($entry.Value)) {
            Fail "$($entry.Key) is required. Provide real DID signatures and a client-signed SPL transaction; the script will not substitute a mock chain or signature."
        }
    }

    $payHeaders = @{
        "X-StablePay-DID" = $AgentDID
        "X-StablePay-Signature" = $GatewaySignature
        "X-StablePay-Timestamp" = $Timestamp
        "X-StablePay-Nonce" = $Nonce
        "X-Idempotency-Key" = $IdempotencyKey
        "X-Request-Id" = "e2e-$([guid]::NewGuid().ToString())"
        "X-Trace-Id" = "trace-$([guid]::NewGuid().ToString())"
    }
    $payBody = @{
        agent_did = $AgentDID; skill_did = $SkillDID; amount = $Amount; currency = $Currency;
        signature = $PaymentSignature; timestamp = $Timestamp; nonce = $Nonce;
        signed_tx_base64 = $SignedTxBase64
    }
    $payResponse = Invoke-Json "POST" "http://127.0.0.1:8080/api/v1/pay" $payHeaders $payBody
    $payData = if ($payResponse.data) { $payResponse.data } else { $payResponse }
    $txID = [string]$payData.tx_id
    if ([string]::IsNullOrWhiteSpace($txID)) { Fail "payment response did not contain tx_id" }
    Write-Host "[deterministic-e2e] payment initiated tx_id=$txID status=$($payData.status)"

    $status = $null
    $statusDeadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        Start-Sleep -Seconds 3
        Push-Location $replayDir
        try {
            $statusOutput = & go run ./cmd/query-payment -address 127.0.0.1:8888 -tx-id $txID 2>&1
            if ($LASTEXITCODE -ne 0) { Fail "payment-service status RPC failed for tx_id=$txID" }
            $status = ($statusOutput -join "") | ConvertFrom-Json
        } finally { Pop-Location }
        if ([string]$status.status -in @("CONFIRMED", "COMPLETED", "FAILED")) { break }
    } while ((Get-Date) -lt $statusDeadline)
    if ($null -eq $status -or [string]$status.status -notin @("CONFIRMED", "COMPLETED")) {
        Fail "Solana Devnet payment did not reach confirmed/completed: status=$($status.status)"
    }
    if ([string]::IsNullOrWhiteSpace([string]$status.tx_hash)) { Fail "confirmed payment has no chain tx_hash" }
    Write-Host "[deterministic-e2e] settlement status=$($status.status) tx_hash=$($status.tx_hash)"

    $verifyHeaders = @{ "X-API-Key" = "stablepay-dev-key" }
    $verifyDeadline = (Get-Date).AddSeconds(60)
    $verify = $null
    do {
        Start-Sleep -Seconds 2
        $verifyResponse = Invoke-Json "GET" ("http://127.0.0.1:8080/api/v1/verify?agent_did=" + [uri]::EscapeDataString($AgentDID) + "&skill_did=" + [uri]::EscapeDataString($SkillDID)) $verifyHeaders
        $verify = if ($verifyResponse.data) { $verifyResponse.data } else { $verifyResponse }
    } while (-not $verify.purchased -and (Get-Date) -lt $verifyDeadline)
    if (-not $verify.purchased) { Fail "verification-service did not project the purchase" }
    $proofResponse = Invoke-Json "GET" ("http://127.0.0.1:8080/api/v1/verify/proof?agent_did=" + [uri]::EscapeDataString($AgentDID) + "&skill_did=" + [uri]::EscapeDataString($SkillDID)) $verifyHeaders
    $proof = if ($proofResponse.data) { $proofResponse.data } else { $proofResponse }
    if (-not $proof.purchased -or [string]$proof.tx_id -ne $txID) { Fail "purchase proof does not match payment tx_id" }
    Write-Host "[deterministic-e2e] purchase projected identity=$AgentDID|$SkillDID proof_tx_id=$($proof.tx_id)"

    # Capture the exact deterministic event identity from the producer log.
    $paymentLog = Join-Path $root ".local-run\logs\payment-service.out.log"
    $eventLine = Get-Content -LiteralPath $paymentLog -Tail 200 | Select-String "event_id=.*tx_id=$txID|tx_id=$txID.*event_id=" | Select-Object -Last 1
    if (-not $eventLine) { Fail "could not find the published event_id for $txID in payment-service log; replay was not attempted" }
    $eventMatch = [regex]::Match($eventLine.Line, "event_id[=: ]+([0-9a-fA-F-]{36})")
    if (-not $eventMatch.Success) { Fail "published event_id was not parseable; replay was not attempted" }
    $eventID = $eventMatch.Groups[1].Value

    $amountMinor = [int64]([decimal]$Amount * 1000000)
    $occurredAt = (Get-Date).ToUniversalTime().ToString("o")
    $replayEvent = [ordered]@{
        event_id = $eventID; event_type = "payment.success"; schema_version = 1; idempotency_key = "$AgentDID`:$SkillDID`:$Nonce";
        tx_id = $txID; agent_did = $AgentDID; skill_did = $SkillDID; amount_minor = $amountMinor; currency = $Currency;
        tx_hash = [string]$status.tx_hash; status = "COMPLETED"; occurred_at = $occurredAt; confirmed_at = $occurredAt;
        request_id = "e2e-replay"; trace_id = "e2e-replay"
    }
    New-Item -ItemType Directory -Force -Path (Split-Path $payloadFile) | Out-Null
    $replayEvent | ConvertTo-Json -Depth 5 | Set-Content -Encoding UTF8 -LiteralPath $payloadFile
    Push-Location $replayDir
    try {
        & go run ./cmd/replay-payment-event -nameserver 127.0.0.1:9876 -file $payloadFile
        if ($LASTEXITCODE -ne 0) { Fail "RocketMQ event replay failed" }
    } finally { Pop-Location }
    Start-Sleep -Seconds 5
    $proofAfterReplayResponse = Invoke-Json "GET" ("http://127.0.0.1:8080/api/v1/verify/proof?agent_did=" + [uri]::EscapeDataString($AgentDID) + "&skill_did=" + [uri]::EscapeDataString($SkillDID)) $verifyHeaders
    $proofAfterReplay = if ($proofAfterReplayResponse.data) { $proofAfterReplayResponse.data } else { $proofAfterReplayResponse }
    if ([string]$proofAfterReplay.tx_id -ne $txID) { Fail "event replay changed purchase projection" }
    Write-Host "[deterministic-e2e] replayed event_id=$eventID; purchase projection remained idempotent"
    Write-Host "[deterministic-e2e] PASS real MySQL + Redis + RocketMQ + six services + Solana Devnet"
} finally {
    if (-not $KeepServices -and $servicesStarted) {
        & powershell -NoProfile -ExecutionPolicy Bypass -File $stopScript
    }
    if (-not $KeepInfra -and $infraStarted) {
        Push-Location $infraDir
        try { & $dockerExe compose -f $composeFile down } finally { Pop-Location }
    }
}
