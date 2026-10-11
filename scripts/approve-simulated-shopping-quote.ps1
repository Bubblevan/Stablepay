param(
    [Parameter(Mandatory = $true)]
    [string]$QuoteId
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$stateRoot = Join-Path $repoRoot '.local-run\agent-shopping'
$secretsPath = Join-Path $stateRoot 'secrets.env'
$approvalKeyPath = Join-Path $stateRoot 'buyer-approval.key'
$apiBase = 'http://127.0.0.1:18090'

if ($QuoteId -notmatch '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,95}$') {
    throw 'QuoteId is not a valid local simulation quote ID.'
}
if (-not (Test-Path -LiteralPath $secretsPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $approvalKeyPath -PathType Leaf)) {
    throw 'Local simulation credentials are missing. Start the isolated shopping simulation first.'
}

$secrets = @{}
foreach ($line in [IO.File]::ReadAllLines($secretsPath)) {
    if ([string]::IsNullOrWhiteSpace($line)) { continue }
    $separator = $line.IndexOf('=')
    if ($separator -lt 1) { throw 'Local simulation secrets file has an invalid line.' }
    $secrets[$line.Substring(0, $separator)] = $line.Substring($separator + 1)
}
$apiKey = $secrets['SHOP_SIM_API_KEY']
$buyerApprovalKey = [IO.File]::ReadAllText($approvalKeyPath).Trim()
if ($apiKey.Length -lt 32 -or $buyerApprovalKey.Length -lt 32) {
    throw 'Local simulation credentials are invalid; no credentials were printed.'
}

$headers = @{
    'X-Simulation-API-Key' = $apiKey
    'Accept' = 'application/json'
}
$quoteUri = "$apiBase/v1/quotes/$([uri]::EscapeDataString($QuoteId))"
$quoteResponse = Invoke-RestMethod -Method Get -Uri $quoteUri -Headers $headers -TimeoutSec 15
$quote = $quoteResponse.quote
if ($null -eq $quote -or $quote.quote_id -ne $QuoteId -or [long]$quote.amount_minor -le 0) {
    throw 'The API did not return the requested immutable quote.'
}

Write-Host ''
Write-Host 'Review this isolated simulation quote before deciding:'
Write-Host "  Merchant: $($quote.merchant_id)"
Write-Host "  Product:  $($quote.product_title) [$($quote.match_type)]"
if (-not [string]::IsNullOrWhiteSpace($quote.substitution_notice)) {
    Write-Host "  Substitution: $($quote.substitution_notice)"
}
Write-Host "  SKU:      $($quote.sku_id)"
Write-Host "  Region:   $($quote.region)"
Write-Host "  Total:    $($quote.amount_minor) $($quote.currency) (minor units)"
Write-Host "  Expires:  $($quote.expires_at)"
Write-Host ''

$decision = Read-Host "Type exactly 'APPROVE $($quote.amount_minor)' to approve, or 'REJECT' to decline"
if ($decision -ceq "APPROVE $($quote.amount_minor)") {
    $accepted = $true
} elseif ($decision -ceq 'REJECT') {
    $accepted = $false
} else {
    throw 'No buyer decision was recorded because the confirmation phrase did not match.'
}

$body = @{
    buyer_id = $quote.buyer_id
    accepted = $accepted
    amount_minor = [long]$quote.amount_minor
    idempotency_key = "human-decision-$QuoteId"
} | ConvertTo-Json -Compress
$confirmHeaders = @{
    'X-Simulation-API-Key' = $apiKey
    'X-Simulation-Buyer-Approval-Key' = $buyerApprovalKey
    'Accept' = 'application/json'
}
$confirmUri = "$apiBase/v1/quotes/$([uri]::EscapeDataString($QuoteId))/confirm"
$confirmationResponse = Invoke-RestMethod -Method Post -Uri $confirmUri -Headers $confirmHeaders `
    -ContentType 'application/json' -Body $body -TimeoutSec 15

Write-Host "Buyer decision recorded: accepted=$($confirmationResponse.confirmation.accepted)"
if ($accepted) {
    Write-Host "Confirmation ID: $($confirmationResponse.confirmation.confirmation_id)"
    Write-Host 'No payment was created. A separate simulation checkout is still required.'
} else {
    Write-Host 'The quote was rejected. Do not submit it to checkout.'
}
