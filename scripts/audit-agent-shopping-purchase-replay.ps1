param(
    [Parameter(Mandatory = $true)]
    [string]$RunDir,
    [ValidateRange(1, 10000)]
    [int]$ExpectedAttempts = 300
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Assert-Condition {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-Equal {
    param([object]$Actual, [object]$Expected, [string]$Label)
    if ($Actual -ne $Expected) {
        throw "$Label mismatch: expected '$Expected', observed '$Actual'"
    }
}

function Get-OptionalProperty {
    param([object]$Object, [string]$Name)
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property) { return $null }
    return $property.Value
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
$auditSummary = Get-Content -LiteralPath $paths['audit-summary.json'] -Raw | ConvertFrom-Json
$schemaVersion = [string]$manifest.schema_version
Assert-Condition ($schemaVersion -in @('stablepay.agent-shopping-http-purchase-replay.v2', 'stablepay.agent-shopping-http-purchase-replay.v3')) 'Unsupported purchase-replay manifest schema'
$isRetrySchema = $schemaVersion -eq 'stablepay.agent-shopping-http-purchase-replay.v3'
Assert-Equal $manifest.run_id (Split-Path -Leaf $resolvedRunDir) 'Run ID'
Assert-Equal $manifest.status 'PASS' 'Manifest status'
Assert-Equal $metrics.status 'PASS' 'Metrics status'
Assert-Equal $metrics.run_id $manifest.run_id 'Metrics run ID'
Assert-Equal ([bool]$manifest.audit_passed) $true 'Manifest audit status'
Assert-Equal $manifest.payment_provider 'simulation-only' 'Payment provider'
Assert-Equal ([bool]$manifest.real_settlement) $false 'Real settlement flag'
Assert-Equal $manifest.fulfillment_provider 'simulation-only' 'Fulfillment provider'
Assert-Equal ([bool]$manifest.real_fulfillment) $false 'Real fulfillment flag'
Assert-Equal ([bool]$manifest.real_refund) $false 'Real refund flag'
Assert-Equal ([int]$manifest.new_model_calls) 0 'New model calls'
Assert-Equal ([int]$manifest.task_count) $ExpectedAttempts 'Manifest task count'
Assert-Equal ([int]$metrics.logical_purchase_attempts) $ExpectedAttempts 'Metrics attempt count'
Assert-Equal ([int]$metrics.unique_orders) $ExpectedAttempts 'Metrics unique orders'
Assert-Equal ([int]$metrics.unique_mock_payments) $ExpectedAttempts 'Metrics unique mock payments'
Assert-Equal ([bool]$metrics.audit.passed) $true 'Metrics audit status'
Assert-Equal ([bool]$auditSummary.passed) $true 'Audit-summary status'

$trials = [System.Collections.Generic.List[object]]::new()
$lineNumber = 0
foreach ($line in Get-Content -LiteralPath $paths['trials.jsonl']) {
    $lineNumber++
    if ([string]::IsNullOrWhiteSpace($line)) { throw "trials.jsonl line $lineNumber is blank" }
    try {
        $trial = $line | ConvertFrom-Json
    } catch {
        throw "trials.jsonl line $lineNumber is invalid JSON: $($_.Exception.Message)"
    }
    foreach ($field in @('task_id', 'order_idempotency_key', 'order_id', 'payment_id', 'fault_mode', 'order_status', 'mock_payment_status', 'final_order_status', 'delivery_failure_predeclared', 'os_process_restarted_before_retry')) {
        Assert-Condition (-not [string]::IsNullOrWhiteSpace([string]$trial.$field)) "Trial line $lineNumber lacks $field"
    }
    Assert-Condition ([double]$trial.end_to_end_latency_ms -ge 0) "Trial line $lineNumber has invalid latency"
    $fulfillmentLatency = Get-OptionalProperty $trial 'fulfillment_http_latency_ms'
    if ($null -ne $fulfillmentLatency) {
        Assert-Condition ([double]$fulfillmentLatency -ge 0) "Trial line $lineNumber has invalid fulfillment latency"
    }
    Assert-Equal ([int]$trial.concurrent_checkout_requests) 2 "Trial line $lineNumber checkout request count"
    $trials.Add($trial)
}
Assert-Equal $trials.Count $ExpectedAttempts 'Trial row count'
Assert-Equal ([int]$manifest.fault_schedule.os_process_restart_after_response_loss) 1 'Manifest OS process restart schedule'
Assert-Equal ([int]$manifest.os_process_restarts_after_response_loss) 1 'Manifest OS process restart count'
Assert-Equal ([int]$metrics.os_process_restarts_after_response_loss) 1 'Metrics OS process restart count'

foreach ($field in @('task_id', 'order_idempotency_key', 'order_id', 'payment_id')) {
    $uniqueCount = @($trials | ForEach-Object { [string]$_.$field } | Sort-Object -Unique).Count
    Assert-Equal $uniqueCount $ExpectedAttempts "Unique $field count"
}

$expectedFaultModes = @{
    'POST_COMMIT_RESPONSE_LOSS_RETRY' = [Math]::Floor($ExpectedAttempts / 3)
    'POST_COMMIT_RESPONSE_LOSS_API_RECREATE_RETRY' = [Math]::Floor($ExpectedAttempts / 3)
    'CONCURRENT_DUPLICATE_REPLAY' = $ExpectedAttempts - 2 * [Math]::Floor($ExpectedAttempts / 3)
}
$faultCounts = @{}
foreach ($group in ($trials | Group-Object -Property fault_mode)) { $faultCounts[$group.Name] = $group.Count }
Assert-Equal $faultCounts.Count $expectedFaultModes.Count 'Fault-mode count'
foreach ($mode in $expectedFaultModes.Keys) {
    Assert-Equal ([int]$faultCounts[$mode]) ([int]$expectedFaultModes[$mode]) "Fault mode $mode"
}
Assert-Equal ([int]$manifest.fault_schedule.post_commit_response_loss_retry) ([int]$expectedFaultModes['POST_COMMIT_RESPONSE_LOSS_RETRY']) 'Manifest retry fault schedule'
Assert-Equal ([int]$manifest.fault_schedule.post_commit_response_loss_api_recreate_retry) ([int]$expectedFaultModes['POST_COMMIT_RESPONSE_LOSS_API_RECREATE_RETRY']) 'Manifest API-recreation fault schedule'
Assert-Equal ([int]$manifest.fault_schedule.concurrent_duplicate_request_pair) ([int]$expectedFaultModes['CONCURRENT_DUPLICATE_REPLAY']) 'Manifest concurrent fault schedule'
Assert-Equal ([int]$metrics.concurrent_duplicate_request_pairs) ([int]$expectedFaultModes['CONCURRENT_DUPLICATE_REPLAY']) 'Metrics concurrent duplicate pair count'

$settled = 0
$declined = 0
$fulfilled = 0
$compensated = 0
$predeclaredDeliveryFailures = 0
$predeclaredTransientFaults = 0
$observedTransientFailures = 0
$providerDeliveryAttempts = 0
$fulfillmentRetryRequests = 0
$lostResponses = 0
$apiRecreated = 0
$osProcessRestarted = 0
foreach ($trial in $trials) {
    switch ([string]$trial.mock_payment_status) {
        'SETTLED' {
            $settled++
            Assert-Equal $trial.order_status 'PAID' "Order status for $($trial.task_id)"
            $fulfillmentStatus = Get-OptionalProperty $trial 'fulfillment_status'
            $refundStatus = Get-OptionalProperty $trial 'mock_refund_status'
            switch ([string]$fulfillmentStatus) {
                'DELIVERED' {
                    $fulfilled++
                    Assert-Equal $trial.final_order_status 'FULFILLED' "Final order status for $($trial.task_id)"
                    if ($isRetrySchema) {
                        $scheduledTransient = [bool]$trial.delivery_failure_predeclared
                        $attempts = [int](Get-OptionalProperty $trial 'fulfillment_provider_attempts')
                        $transientFailures = [int](Get-OptionalProperty $trial 'transient_delivery_failures_observed')
                        $retryRequests = [int](Get-OptionalProperty $trial 'fulfillment_retry_http_requests')
                        if ($scheduledTransient) {
                            $predeclaredTransientFaults++
                            Assert-Condition ($transientFailures -ge 1) "Scheduled transient fault was not observed for $($trial.task_id)"
                            Assert-Equal $attempts ($transientFailures + 1) "Provider attempts for $($trial.task_id)"
                            Assert-Condition ($retryRequests -ge $transientFailures) "Retry request count is below injected failures for $($trial.task_id)"
                        } else {
                            Assert-Equal $transientFailures 0 "Unexpected transient failure for $($trial.task_id)"
                            Assert-Equal $attempts 1 "Unfaulted provider attempt count for $($trial.task_id)"
                            Assert-Equal $retryRequests 0 "Unfaulted retry-request count for $($trial.task_id)"
                        }
                        $observedTransientFailures += $transientFailures
                        $providerDeliveryAttempts += $attempts
                        $fulfillmentRetryRequests += $retryRequests
                    } else {
                        Assert-Equal ([bool]$trial.delivery_failure_predeclared) $false "Delivery-fault schedule for $($trial.task_id)"
                    }
                    Assert-Condition ([string]::IsNullOrWhiteSpace([string]$refundStatus)) "Delivered order $($trial.task_id) unexpectedly has a refund"
                }
                'FAILED_COMPENSATED' {
                    $compensated++
                    $predeclaredDeliveryFailures++
                    Assert-Condition (-not $isRetrySchema) "Retry-schema run unexpectedly compensated a scheduled transient delivery fault for $($trial.task_id)"
                    Assert-Equal $trial.final_order_status 'REFUNDED' "Final order status for $($trial.task_id)"
                    Assert-Equal ([bool]$trial.delivery_failure_predeclared) $true "Delivery-fault schedule for $($trial.task_id)"
                    Assert-Equal $refundStatus 'REFUNDED' "Refund status for $($trial.task_id)"
                }
                default { throw "Unexpected fulfillment status '$fulfillmentStatus' for settled task $($trial.task_id)" }
            }
        }
        'DECLINED' {
            $declined++
            Assert-Equal $trial.order_status 'PAYMENT_FAILED' "Order status for $($trial.task_id)"
            Assert-Equal $trial.final_order_status 'PAYMENT_FAILED' "Final order status for $($trial.task_id)"
            Assert-Equal ([bool]$trial.delivery_failure_predeclared) $false "Delivery-fault schedule for $($trial.task_id)"
            Assert-Condition ([string]::IsNullOrWhiteSpace([string](Get-OptionalProperty $trial 'fulfillment_status'))) "Declined order $($trial.task_id) unexpectedly has fulfillment"
            Assert-Condition ([string]::IsNullOrWhiteSpace([string](Get-OptionalProperty $trial 'mock_refund_status'))) "Declined order $($trial.task_id) unexpectedly has a refund"
            if ($isRetrySchema) {
                Assert-Equal ([int](Get-OptionalProperty $trial 'fulfillment_provider_attempts')) 0 "Declined-order provider attempts for $($trial.task_id)"
                Assert-Equal ([int](Get-OptionalProperty $trial 'transient_delivery_failures_observed')) 0 "Declined-order transient failures for $($trial.task_id)"
                Assert-Equal ([int](Get-OptionalProperty $trial 'fulfillment_retry_http_requests')) 0 "Declined-order retry requests for $($trial.task_id)"
            }
        }
        default { throw "Unexpected payment status '$($trial.mock_payment_status)' for $($trial.task_id)" }
    }
    if ([bool]$trial.post_commit_response_lost) { $lostResponses++ }
    if ([bool]$trial.api_recreated_before_retry) { $apiRecreated++ }
    if ([bool]$trial.os_process_restarted_before_retry) {
        $osProcessRestarted++
        Assert-Equal ([bool]$trial.post_commit_response_lost) $true "OS-process response-loss flag for $($trial.task_id)"
        Assert-Equal ([bool]$trial.api_recreated_before_retry) $true "OS-process API-recreation flag for $($trial.task_id)"
        Assert-Equal $trial.fault_mode 'POST_COMMIT_RESPONSE_LOSS_API_RECREATE_RETRY' "OS-process fault mode for $($trial.task_id)"
    }
    switch ([string]$trial.fault_mode) {
        'POST_COMMIT_RESPONSE_LOSS_RETRY' {
            Assert-Equal ([bool]$trial.post_commit_response_lost) $true "Response-loss flag for $($trial.task_id)"
            Assert-Equal ([bool]$trial.api_recreated_before_retry) $false "API-recreation flag for $($trial.task_id)"
        }
        'POST_COMMIT_RESPONSE_LOSS_API_RECREATE_RETRY' {
            Assert-Equal ([bool]$trial.post_commit_response_lost) $true "Response-loss flag for $($trial.task_id)"
            Assert-Equal ([bool]$trial.api_recreated_before_retry) $true "API-recreation flag for $($trial.task_id)"
        }
        'CONCURRENT_DUPLICATE_REPLAY' {
            Assert-Equal ([bool]$trial.post_commit_response_lost) $false "Response-loss flag for $($trial.task_id)"
            Assert-Equal ([bool]$trial.api_recreated_before_retry) $false "API-recreation flag for $($trial.task_id)"
        }
    }
}
Assert-Equal ($settled + $declined) $ExpectedAttempts 'Payment outcome partition'
Assert-Equal ($fulfilled + $compensated) $settled 'Settled-order fulfillment outcome partition'
Assert-Equal ($fulfilled + $compensated + $declined) $ExpectedAttempts 'End-to-end final outcome partition'
Assert-Equal $settled ([int]$metrics.settled_unique_orders) 'Settled trial count'
Assert-Equal $declined ([int]$metrics.declined_unique_orders) 'Declined trial count'
Assert-Equal $settled ([int]$metrics.fulfillment_attempts) 'Fulfillment attempt count'
Assert-Equal $fulfilled ([int]$metrics.fulfilled_orders) 'Delivered order count'
Assert-Equal $compensated ([int]$metrics.compensated_delivery_failures) 'Compensated delivery-failure count'
if ($isRetrySchema) {
    Assert-Equal $predeclaredTransientFaults ([int]$metrics.predeclared_transient_delivery_faults) 'Predeclared transient delivery-fault count'
    Assert-Equal $predeclaredTransientFaults ([int]$manifest.predeclared_transient_delivery_fault_count) 'Manifest transient delivery-fault count'
    Assert-Equal $predeclaredTransientFaults ([int]$manifest.fault_schedule.predeclared_transient_delivery_fault_cases) 'Manifest transient-fault schedule'
    Assert-Equal $predeclaredTransientFaults $observedTransientFailures 'Observed retryable failure count'
    Assert-Equal $providerDeliveryAttempts ([int]$metrics.provider_delivery_attempts) 'Provider delivery-attempt count'
    Assert-Equal $observedTransientFailures ([int]$metrics.transient_delivery_failures) 'Transient delivery-failure metric'
    Assert-Equal $predeclaredTransientFaults ([int]$metrics.recovered_delivery_faults) 'Recovered delivery-fault count'
    Assert-Equal $fulfillmentRetryRequests ([int]$metrics.fulfillment_retry_http_requests) 'Fulfillment retry HTTP request count'
} else {
    Assert-Equal $predeclaredDeliveryFailures ([int]$metrics.predeclared_delivery_failures) 'Predeclared delivery-failure count'
    Assert-Equal $predeclaredDeliveryFailures ([int]$manifest.predeclared_delivery_failure_count) 'Manifest predeclared delivery-failure count'
    Assert-Equal $predeclaredDeliveryFailures ([int]$manifest.fault_schedule.predeclared_delivery_failure_cases) 'Manifest delivery-fault schedule'
}
Assert-Equal $settled ([int]$metrics.fulfillment_idempotent_replay_requests) 'Fulfillment idempotent replay count'
Assert-Equal $settled ([int]$metrics.fulfillment_conflict_checks) 'Fulfillment conflict-check count'
$expectedFulfillmentHTTPRequests = (3 * $settled) + $fulfillmentRetryRequests
Assert-Equal $expectedFulfillmentHTTPRequests ([int]$metrics.fulfillment_http_requests) 'Fulfillment HTTP request count'
Assert-Equal ([bool]$metrics.audit.fulfillment_audit.passed) $true 'Fulfillment SQL audit status'
Assert-Equal ([bool]$manifest.fulfillment_audit_passed) $true 'Manifest fulfillment audit status'
Assert-Equal $osProcessRestarted 1 'OS process restart trial count'
Assert-Equal $lostResponses ([int]$metrics.injected_post_commit_response_losses) 'Response-loss count'
Assert-Equal $apiRecreated ([int]$metrics.api_recreations_after_response_loss) 'API recreation count'
$checkoutRequests = [int](($trials | Measure-Object -Property concurrent_checkout_requests -Sum).Sum)
Assert-Equal $checkoutRequests ([int]$metrics.checkout_http_requests) 'Checkout HTTP request count'

$audit = $metrics.audit
foreach ($property in $audit.lifecycle_audit.expected_counts.PSObject.Properties) {
    $name = $property.Name
    Assert-Equal ([int]$audit.lifecycle_audit.expected_counts.$name) ([int]$audit.lifecycle_audit.observed_counts.$name) "Audit row count $name"
}
Assert-Equal ([int]$audit.lifecycle_audit.expected_counts.tasks) $ExpectedAttempts 'Audit expected tasks'
Assert-Equal ([int]$audit.lifecycle_audit.observed_counts.tasks) $ExpectedAttempts 'Audit observed tasks'
Assert-Equal (@($audit.lifecycle_audit.invariant_violations.PSObject.Properties).Count) 0 'Invariant violation count'
Assert-Equal ([int]$audit.lifecycle_audit.task_statuses.PAID) $settled 'Audited PAID status count'
Assert-Equal ([int]$audit.lifecycle_audit.task_statuses.PAYMENT_FAILED) $declined 'Audited PAYMENT_FAILED status count'
Assert-Equal ([int]$audit.lifecycle_audit.audit_actions.MOCK_PAYMENT_SETTLED) $settled 'Audited settlement action count'
Assert-Equal ([int]$audit.lifecycle_audit.audit_actions.MOCK_PAYMENT_DECLINED) $declined 'Audited decline action count'
foreach ($counter in @('duplicate_order_idempotency_groups', 'duplicate_orders_per_task', 'duplicate_payments_per_order', 'duplicate_settlements_per_order', 'amount_or_currency_mismatches')) {
    Assert-Equal ([int]$audit.$counter) 0 "Audit counter $counter"
}
Assert-Equal ([int]$audit.observed_settlements) $settled 'Audited settlement total'
Assert-Equal ([int]$audit.observed_declines) $declined 'Audited decline total'
Assert-Equal ([int]$audit.fulfillment_audit.observed_fulfilled_orders) $fulfilled 'Audited delivered-order total'
Assert-Equal ([int]$audit.fulfillment_audit.observed_compensated_orders) $compensated 'Audited compensated-order total'
Assert-Equal ([int]$audit.fulfillment_audit.observed_mock_refunds) $compensated 'Audited mock-refund total'
if ($isRetrySchema) {
    Assert-Equal ([int]$audit.fulfillment_audit.observed_provider_attempts) $providerDeliveryAttempts 'Audited provider-attempt total'
    Assert-Equal ([int]$audit.fulfillment_audit.retryable_failure_attempts) $observedTransientFailures 'Audited transient-failure total'
}
Assert-Equal ([bool]$auditSummary.fulfillment_audit.passed) ([bool]$audit.fulfillment_audit.passed) 'Fulfillment audit-summary status cross-check'
$fulfillmentAuditCounters = @('duplicate_fulfillments_per_order', 'duplicate_refunds_per_order', 'settled_payments_without_fulfillment', 'declined_payments_with_fulfillment_or_refund', 'final_order_or_task_state_mismatches', 'refund_amount_or_payment_mismatches', 'reservation_state_mismatches', 'non_simulation_fulfillment_effects')
if ($isRetrySchema) {
    $fulfillmentAuditCounters += @('duplicate_attempt_ordinals', 'attempt_sequence_gaps', 'retryable_failures_without_retry', 'orphan_fulfillment_attempts', 'final_attempt_count_mismatches', 'terminal_attempt_outcome_mismatches', 'attempt_idempotency_mismatches')
}
foreach ($counter in $fulfillmentAuditCounters) {
    Assert-Equal ([int]$audit.fulfillment_audit.$counter) 0 "Fulfillment audit counter $counter"
    Assert-Equal ([int]$auditSummary.fulfillment_audit.$counter) ([int]$audit.fulfillment_audit.$counter) "Fulfillment audit-summary counter $counter"
}
Assert-Equal (@($audit.fulfillment_audit.invariant_violations.PSObject.Properties).Count) 0 'Fulfillment invariant violation count'
$fulfillmentQueryErrors = $audit.fulfillment_audit.PSObject.Properties['query_errors']
$fulfillmentQueryErrorCount = 0
if ($null -ne $fulfillmentQueryErrors -and $null -ne $fulfillmentQueryErrors.Value) {
    $fulfillmentQueryErrorCount = @($fulfillmentQueryErrors.Value.PSObject.Properties).Count
}
Assert-Equal $fulfillmentQueryErrorCount 0 'Fulfillment SQL query error count'
Assert-Equal ([bool]$auditSummary.passed) ([bool]$audit.passed) 'Audit summary cross-check'
Assert-Equal ([int]$auditSummary.observed_settlements) ([int]$audit.observed_settlements) 'Audit summary settlement cross-check'
Assert-Equal ([int]$auditSummary.observed_declines) ([int]$audit.observed_declines) 'Audit summary decline cross-check'
$queryErrors = $audit.PSObject.Properties['query_errors']
$queryErrorCount = 0
if ($null -ne $queryErrors -and $null -ne $queryErrors.Value) {
    $queryErrorCount = @($queryErrors.Value.PSObject.Properties).Count
}
Assert-Equal $queryErrorCount 0 'SQL query error count'
Assert-Equal (@($auditSummary.lifecycle_audit.invariant_violations.PSObject.Properties).Count) 0 'Captured lifecycle invariant violation count'
foreach ($counter in @('duplicate_order_idempotency_groups', 'duplicate_orders_per_task', 'duplicate_payments_per_order', 'duplicate_settlements_per_order', 'amount_or_currency_mismatches')) {
    Assert-Equal ([int]$auditSummary.$counter) ([int]$audit.$counter) "Audit-summary counter cross-check $counter"
}
foreach ($property in $audit.lifecycle_audit.expected_counts.PSObject.Properties) {
    $name = $property.Name
    Assert-Equal ([int]$auditSummary.lifecycle_audit.expected_counts.$name) ([int]$audit.lifecycle_audit.expected_counts.$name) "Audit-summary expected row count $name"
    Assert-Equal ([int]$auditSummary.lifecycle_audit.observed_counts.$name) ([int]$audit.lifecycle_audit.observed_counts.$name) "Audit-summary observed row count $name"
}

$artifactHashes = @{}
foreach ($name in $requiredFiles) {
    $artifactHashes[$name] = 'sha256:' + (Get-FileHash -LiteralPath $paths[$name] -Algorithm SHA256).Hash.ToLowerInvariant()
}

[ordered]@{
    status = 'PASS'
    run_id = $manifest.run_id
    attempts = $trials.Count
    fault_modes = $faultCounts
    settled = $settled
    settlement_rate_pct = [Math]::Round((100.0 * $settled / $ExpectedAttempts), 2)
    declined = $declined
    delivered = $fulfilled
    end_to_end_fulfilled_orders = $fulfilled
    end_to_end_fulfillment_rate_pct = [Math]::Round((100.0 * $fulfilled / $ExpectedAttempts), 2)
    compensated_delivery_failures = $compensated
    compensated_share_of_attempts_pct = [Math]::Round((100.0 * $compensated / $ExpectedAttempts), 2)
    fulfilled_share_of_settled_pct = [Math]::Round((100.0 * $fulfilled / $settled), 2)
    predeclared_transient_faults = $predeclaredTransientFaults
    transient_delivery_failures = $observedTransientFailures
    provider_delivery_attempts = $providerDeliveryAttempts
    fulfillment_retry_http_requests = $fulfillmentRetryRequests
    final_outcome_partition = [ordered]@{
        fulfilled = $fulfilled
        refunded_and_restocked = $compensated
        payment_declined = $declined
        total = $ExpectedAttempts
    }
    predeclared_delivery_failures = $predeclaredDeliveryFailures
    fulfillment_http_requests = [int]$metrics.fulfillment_http_requests
    fulfillment_http_p50_ms = [double]$metrics.fulfillment_http_p50_ms
    fulfillment_http_p95_ms = [double]$metrics.fulfillment_http_p95_ms
    injected_response_losses = $lostResponses
    api_recreations = $apiRecreated
    os_process_restarts = $osProcessRestarted
    checkout_http_requests = $checkoutRequests
    unique_order_keys = $ExpectedAttempts
    unique_orders = $ExpectedAttempts
    unique_payment_ids = $ExpectedAttempts
    duplicate_or_amount_currency_violations = 0
    artifact_sha256 = $artifactHashes
    audit_boundary = 'OFFLINE_RECONCILIATION_OF_SAVED_SUMMARY_AND_TRIALS_NOT_A_FRESH_DATABASE_QUERY'
} | ConvertTo-Json -Depth 10
