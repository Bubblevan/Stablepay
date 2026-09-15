[CmdletBinding()]
param(
    [switch]$SkipUnitGate,
    [switch]$SkipStart,
    [switch]$KeepServices,
    [switch]$KeepInfra,
    [switch]$ManualInputs,
    [bool]$AutoPrepareIdentity = $(if ($env:STABLEPAY_E2E_AUTO_PREPARE -eq "0") { $false } else { $true }),
    [string]$WalletPath = $env:STABLEPAY_HOTWALLET_PATH,
    [string]$AgentKeypairPath = $env:STABLEPAY_E2E_AGENT_KEYPAIR_PATH,
    [string]$SkillKeypairPath = $env:STABLEPAY_E2E_SKILL_KEYPAIR_PATH,
    [string]$PreparedInputsPath = $env:STABLEPAY_E2E_PREPARED_INPUTS_PATH,
    [string]$AgentDID = $env:STABLEPAY_E2E_AGENT_DID,
    [string]$SkillDID = $env:STABLEPAY_E2E_SKILL_DID,
    [string]$PaymentSignature = $env:STABLEPAY_E2E_PAYMENT_SIGNATURE,
    [string]$GatewaySignature = $env:STABLEPAY_E2E_GATEWAY_SIGNATURE,
    [string]$Timestamp = $env:STABLEPAY_E2E_TIMESTAMP,
    [string]$Nonce = $env:STABLEPAY_E2E_NONCE,
    [string]$GatewayNonce = $env:STABLEPAY_E2E_GATEWAY_NONCE,
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
$defaultWallet = Join-Path $root "blockchain-adapter\config\hotwallet.json"
$defaultAgentKeypair = Join-Path $root ".local-run\secrets\e2e-agent.json"
$defaultPreparedInputs = Join-Path $root ".local-run\e2e-prepared.json"

if ($ManualInputs) { $AutoPrepareIdentity = $false }
if ([string]::IsNullOrWhiteSpace($WalletPath)) { $WalletPath = $defaultWallet }
if (-not [IO.Path]::IsPathRooted($WalletPath)) { $WalletPath = Join-Path $root $WalletPath }
$WalletPath = [IO.Path]::GetFullPath($WalletPath)
if ([string]::IsNullOrWhiteSpace($AgentKeypairPath)) { $AgentKeypairPath = $defaultAgentKeypair }
if (-not [IO.Path]::IsPathRooted($AgentKeypairPath)) { $AgentKeypairPath = Join-Path $root $AgentKeypairPath }
$AgentKeypairPath = [IO.Path]::GetFullPath($AgentKeypairPath)
if ([string]::IsNullOrWhiteSpace($PreparedInputsPath)) { $PreparedInputsPath = $defaultPreparedInputs }
if (-not [IO.Path]::IsPathRooted($PreparedInputsPath)) { $PreparedInputsPath = Join-Path $root $PreparedInputsPath }
$PreparedInputsPath = [IO.Path]::GetFullPath($PreparedInputsPath)

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

function Invoke-RawJson([string]$Method, [string]$Uri, [hashtable]$Headers, [string]$BodyFile) {
    if (-not (Test-Path -LiteralPath $BodyFile -PathType Leaf)) { Fail "exact JSON body file missing: $BodyFile" }
    $curlArgs = @("--silent", "--show-error", "--max-time", [string]$TimeoutSeconds, "-X", $Method, "-H", "Content-Type: application/json")
    foreach ($header in $Headers.GetEnumerator()) {
        $curlArgs += @("-H", "$($header.Key): $($header.Value)")
    }
    # --data-binary is required here: the Go harness signs these exact bytes
    # and the Gateway hashes the raw request body.
    $curlArgs += @("--data-binary", "@$BodyFile", $Uri)
    $responseText = & curl.exe @curlArgs 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw ($responseText -join "`n")
    }
    if ([string]::IsNullOrWhiteSpace(($responseText -join ""))) { return $null }
    return ($responseText -join "`n") | ConvertFrom-Json
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
$previousVerificationGroup = $env:VERIFICATION_ROCKETMQ_GROUP
$env:VERIFICATION_ROCKETMQ_GROUP = "verification_e2e_$([guid]::NewGuid().ToString('N'))"
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
        $topicOutput = & $dockerExe exec stablepay-rocketmq-broker sh -c "sh /home/rocketmq/rocketmq-5.3.2/bin/mqadmin updateTopic -n rocketmq-nameserver:9876 -c DefaultCluster -t payment_events" 2>&1
        $topicText = $topicOutput -join "`n"
        # mqadmin may print an error while still returning exit code 0 during
        # the short window before the broker registers with NameServer.
        if ($LASTEXITCODE -eq 0 -and $topicText -match "create topic .* success") {
            $topicReady = $true
            break
        }
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

    if ($AutoPrepareIdentity) {
        Write-Host "[deterministic-e2e] preparing separate Agent identity, real DID registrations, Adapter transaction, and signatures"
        $helperArgs = @("run", "./cmd/e2e-client", "-agent-keypair-path", $AgentKeypairPath, "-hot-wallet-path", $WalletPath)
        if (-not [string]::IsNullOrWhiteSpace($SkillKeypairPath)) {
            $helperArgs += @("-skill-keypair-path", $SkillKeypairPath)
        }
        if (-not [string]::IsNullOrWhiteSpace($SkillDID)) {
            $helperArgs += @("-skill-did", $SkillDID)
        }
        $helperArgs += @("-amount", $Amount, "-currency", $Currency, "-output", $PreparedInputsPath)
        Push-Location $replayDir
        try {
            $helperOutput = & go @helperArgs 2>&1
            if ($LASTEXITCODE -ne 0) {
                Fail "real-signing harness could not prepare the request. Devnet funding prerequisite or service error: $($helperOutput -join ' ')"
            }
        } finally { Pop-Location }
        if (-not (Test-Path -LiteralPath $PreparedInputsPath -PathType Leaf)) {
            Fail "real-signing harness completed without prepared input file: $PreparedInputsPath"
        }
        $prepared = Get-Content -Raw -LiteralPath $PreparedInputsPath | ConvertFrom-Json
        $AgentDID = [string]$prepared.agent_did
        $SkillDID = [string]$prepared.skill_did
        $PaymentSignature = [string]$prepared.payment_signature
        $GatewaySignature = [string]$prepared.gateway_signature
        $Timestamp = [string]$prepared.timestamp
        $Nonce = [string]$prepared.nonce
        $GatewayNonce = [string]$prepared.gateway_nonce
        $SignedTxBase64 = [string]$prepared.signed_tx_base64
        $IdempotencyKey = [string]$prepared.idempotency_key
        $Amount = [string]$prepared.amount
        $Currency = [string]$prepared.currency
        $bodyFile = [string]$prepared.body_file
        if (-not [IO.Path]::IsPathRooted($bodyFile)) { $bodyFile = Join-Path $root $bodyFile }
        $bodyFile = [IO.Path]::GetFullPath($bodyFile)
    }

    $required = @{
        AgentDID = $AgentDID; SkillDID = $SkillDID; PaymentSignature = $PaymentSignature;
        GatewaySignature = $GatewaySignature; Timestamp = $Timestamp; Nonce = $Nonce;
        GatewayNonce = $GatewayNonce; SignedTxBase64 = $SignedTxBase64; IdempotencyKey = $IdempotencyKey
    }
    foreach ($entry in $required.GetEnumerator()) {
        if ([string]::IsNullOrWhiteSpace($entry.Value)) {
            Fail "$($entry.Key) is required. Use auto mode or provide real DID signatures, distinct gateway/payment nonces, and a client-signed SPL transaction; the script will not substitute a mock chain or signature."
        }
    }

    $payHeaders = @{
        "X-StablePay-DID" = $AgentDID
        "X-StablePay-Signature" = $GatewaySignature
        "X-StablePay-Timestamp" = $Timestamp
        "X-StablePay-Nonce" = $GatewayNonce
        "X-Idempotency-Key" = $IdempotencyKey
        "X-Request-Id" = "e2e-$([guid]::NewGuid().ToString())"
        "X-Trace-Id" = "trace-$([guid]::NewGuid().ToString())"
    }
    if ($AutoPrepareIdentity) {
        $payResponse = Invoke-RawJson "POST" "http://127.0.0.1:8080/api/v1/pay" $payHeaders $bodyFile
    } else {
        $payBody = @{
            agent_did = $AgentDID; skill_did = $SkillDID; amount = $Amount; currency = $Currency;
            signature = $PaymentSignature; timestamp = $Timestamp; nonce = $Nonce;
            signed_tx_base64 = $SignedTxBase64
        }
        $payResponse = Invoke-Json "POST" "http://127.0.0.1:8080/api/v1/pay" $payHeaders $payBody
    }
    $payData = if ($payResponse.data) { $payResponse.data } else { $payResponse }
    $txID = [string]$payData.tx_id
    if ([string]::IsNullOrWhiteSpace($txID)) {
        $responseDebug = $payResponse | ConvertTo-Json -Depth 8 -Compress
        Fail "payment response did not contain tx_id: $responseDebug"
    }
    Write-Host "[deterministic-e2e] payment initiated tx_id=$txID status=$($payData.status)"

    # Re-submit the exact same business request with a fresh gateway nonce.
    # The payment nonce remains bound to the business signature; the payment
    # service must return the original tx_id from its idempotency record.
    $idempotencyHeaders = @{
        "X-StablePay-DID" = $AgentDID
        "X-StablePay-Signature" = $GatewaySignature
        "X-StablePay-Timestamp" = $Timestamp
        "X-StablePay-Nonce" = "gateway-retry-$([guid]::NewGuid().ToString())"
        "X-Idempotency-Key" = $IdempotencyKey
        "X-Request-Id" = "e2e-idempotency-$([guid]::NewGuid().ToString())"
        "X-Trace-Id" = "trace-idempotency-$([guid]::NewGuid().ToString())"
    }
    $idempotencyResponse = if ($AutoPrepareIdentity) {
        Invoke-RawJson "POST" "http://127.0.0.1:8080/api/v1/pay" $idempotencyHeaders $bodyFile
    } else {
        Invoke-Json "POST" "http://127.0.0.1:8080/api/v1/pay" $idempotencyHeaders $payBody
    }
    $idempotencyData = if ($idempotencyResponse.data) { $idempotencyResponse.data } else { $idempotencyResponse }
    if ([string]$idempotencyData.tx_id -ne $txID) {
        Fail "idempotency replay returned a different tx_id: first=$txID replay=$($idempotencyData.tx_id)"
    }
    Write-Host "[deterministic-e2e] idempotency replay returned the original tx_id=$txID"

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
    # zap's development logger writes to stderr; use that stream for the
    # producer event line instead of the GORM stdout stream.
    $paymentLog = Join-Path $root ".local-run\logs\payment-service.err.log"
    $eventLine = Get-Content -LiteralPath $paymentLog -Tail 200 |
        Select-String "payment event published" |
        Where-Object { $_.Line -match [regex]::Escape($txID) } |
        Select-Object -Last 1
    if (-not $eventLine) { Fail "could not find the published event_id for $txID in payment-service log; replay was not attempted" }
    $eventMatch = [regex]::Match($eventLine.Line, 'event_id.{0,8}([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})')
    if (-not $eventMatch.Success) { Fail "published event_id was not parseable; replay was not attempted" }
    $eventID = $eventMatch.Groups[1].Value

    $amountMinor = if ($AutoPrepareIdentity -and $prepared.amount_minor) {
        [int64]$prepared.amount_minor
    } else {
        [int64]([decimal]$Amount * 100)
    }
    $occurredAt = (Get-Date).ToUniversalTime().ToString("o")
    $replayEvent = [ordered]@{
        event_id = $eventID; event_type = "payment.success"; schema_version = 1; idempotency_key = "$AgentDID`:$SkillDID`:$Nonce";
        tx_id = $txID; agent_did = $AgentDID; skill_did = $SkillDID; amount_minor = $amountMinor; currency = $Currency;
        tx_hash = [string]$status.tx_hash; status = "COMPLETED"; occurred_at = $occurredAt; confirmed_at = $occurredAt;
        request_id = "e2e-replay"; trace_id = "e2e-replay"
    }
    New-Item -ItemType Directory -Force -Path (Split-Path $payloadFile) | Out-Null
    # The Go replay tool expects raw JSON without PowerShell's UTF-8 BOM.
    $replayEvent | ConvertTo-Json -Depth 5 | Set-Content -Encoding ASCII -LiteralPath $payloadFile
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
    if ($null -eq $previousVerificationGroup) {
        Remove-Item Env:VERIFICATION_ROCKETMQ_GROUP -ErrorAction SilentlyContinue
    } else {
        $env:VERIFICATION_ROCKETMQ_GROUP = $previousVerificationGroup
    }
    if (-not $KeepServices -and $servicesStarted) {
        & powershell -NoProfile -ExecutionPolicy Bypass -File $stopScript
    }
    if (-not $KeepInfra -and $infraStarted) {
        Push-Location $infraDir
        try { & $dockerExe compose -f $composeFile down } finally { Pop-Location }
    }
}
