param(
    [Parameter(Mandatory = $true)]
    [string]$RunDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Assert-Condition {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-Equal {
    param([object]$Actual, [object]$Expected, [string]$Label)
    if ($Actual -ne $Expected) { throw "$Label mismatch: expected '$Expected', observed '$Actual'" }
}

$artifactRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\.local-run\agent-shopping\lifecycle-evaluations')).Path
$resolvedRunDir = (Resolve-Path -LiteralPath $RunDir).Path
Assert-Equal (Split-Path -Parent $resolvedRunDir) $artifactRoot 'Run directory parent'

$requiredFiles = @('manifest.json', 'metrics.json', 'audit-summary.json', 'trials.jsonl')
$paths = @{}
foreach ($name in $requiredFiles) {
    $path = Join-Path $resolvedRunDir $name
    $item = Get-Item -LiteralPath $path
    Assert-Condition (-not $item.PSIsContainer) "$name must be a regular file"
    Assert-Condition (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) "$name must not be a reparse point"
    $paths[$name] = $path
}

$manifest = Get-Content -LiteralPath $paths['manifest.json'] -Raw | ConvertFrom-Json
$metrics = Get-Content -LiteralPath $paths['metrics.json'] -Raw | ConvertFrom-Json
$audit = Get-Content -LiteralPath $paths['audit-summary.json'] -Raw | ConvertFrom-Json
Assert-Equal $manifest.schema_version 'stablepay.agent-shopping-os-process-restart-replay.v1' 'Manifest schema'
Assert-Equal $manifest.run_id (Split-Path -Leaf $resolvedRunDir) 'Run ID'
Assert-Equal $manifest.status 'PASS' 'Manifest status'
Assert-Equal ([bool]$manifest.real_settlement) $false 'Real settlement flag'
Assert-Equal ([int]$manifest.new_model_calls) 0 'New model calls'
Assert-Equal ([string]$manifest.payment_provider) 'simulation-only' 'Payment provider'
Assert-Equal ([int]$manifest.task_count) 1 'Manifest task count'
Assert-Equal ([int]$manifest.processes_started) 2 'Process count'
Assert-Equal ([bool]$manifest.post_commit_response_loss) $true 'Response-loss flag'
Assert-Equal ([bool]$manifest.retry_after_os_process_restart) $true 'Restart retry flag'
Assert-Equal ([bool]$manifest.api_key_recorded) $false 'Secret-recording flag'

Assert-Equal $metrics.status 'PASS' 'Metrics status'
Assert-Equal ([int]$metrics.task_count) 1 'Metrics task count'
Assert-Equal ([int]$metrics.unique_orders) 1 'Unique order count'
Assert-Equal ([int]$metrics.unique_mock_payments) 1 'Unique mock payment count'
Assert-Equal ([int]$metrics.os_process_restarts) 1 'OS process restart count'
Assert-Equal ([int]$metrics.response_losses) 1 'Response-loss count'
Assert-Equal ([bool]$metrics.retry_returned_same_order_and_payment) $true 'Idempotent retry identity'
Assert-Equal ([bool]$metrics.real_settlement) $false 'Metrics real settlement flag'
Assert-Equal ([string]$metrics.payment_provider) 'simulation-only' 'Metrics payment provider'

$trials = [System.Collections.Generic.List[object]]::new()
$lineNumber = 0
foreach ($line in Get-Content -LiteralPath $paths['trials.jsonl']) {
    $lineNumber++
    if ([string]::IsNullOrWhiteSpace($line)) { throw "trials.jsonl line $lineNumber is blank" }
    $trial = $line | ConvertFrom-Json
    $trials.Add($trial)
}
Assert-Equal $trials.Count 1 'Trial count'
$trial = $trials[0]
Assert-Equal ([bool]$trial.post_commit_response_lost) $true 'Trial response loss'
Assert-Equal ([bool]$trial.api_recreated_before_retry) $true 'Trial API recreation'
Assert-Equal ([bool]$trial.os_process_restarted_before_retry) $true 'Trial OS process restart'
Assert-Equal $trial.order_id $metrics.order_id 'Trial/metrics order ID'
Assert-Equal $trial.payment_id $metrics.payment_id 'Trial/metrics payment ID'
Assert-Equal $trial.mock_payment_status $metrics.mock_payment_status 'Trial/metrics payment status'

Assert-Equal ([bool]$audit.passed) $true 'Live SQL audit summary status'
Assert-Equal ([bool]$audit.lifecycle_audit.passed) $true 'Lifecycle SQL audit status'
foreach ($counter in @('duplicate_order_idempotency_groups', 'duplicate_orders_per_task', 'duplicate_payments_per_order', 'duplicate_settlements_per_order', 'amount_or_currency_mismatches')) {
    Assert-Equal ([int]$audit.$counter) 0 "SQL audit counter $counter"
}
Assert-Equal ([int]$metrics.duplicate_order_idempotency_groups) 0 'Metrics duplicate idempotency groups'
Assert-Equal ([int]$metrics.duplicate_orders_per_task) 0 'Metrics duplicate orders'
Assert-Equal ([int]$metrics.duplicate_payments_per_order) 0 'Metrics duplicate payments'
Assert-Equal ([int]$metrics.duplicate_settlements_per_order) 0 'Metrics duplicate settlements'
Assert-Equal ([int]$metrics.amount_or_currency_mismatches) 0 'Metrics amount/currency mismatches'

$artifactHashes = @{}
foreach ($name in $requiredFiles) {
    $artifactHashes[$name] = 'sha256:' + (Get-FileHash -LiteralPath $paths[$name] -Algorithm SHA256).Hash.ToLowerInvariant()
}
[ordered]@{
    status = 'PASS'
    run_id = $manifest.run_id
    task_id = $trial.task_id
    os_process_restarts = 1
    response_losses = 1
    payment_provider = 'simulation-only'
    real_settlement = $false
    duplicate_or_amount_currency_violations = 0
    artifact_sha256 = $artifactHashes
    audit_boundary = 'OFFLINE_RECONCILIATION_OF_SAVED_ARTIFACTS; ORIGINAL_TEST_PERFORMED_LIVE_SQL_AUDIT'
} | ConvertTo-Json -Depth 8
