param(
    [Parameter(Mandatory = $true)] [string]$RunDir,
    [string]$DatasetPath = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Assert-Equal([object]$Actual, [object]$Expected, [string]$Name) {
    if ([string]$Actual -ne [string]$Expected) {
        throw "$Name mismatch: expected '$Expected', got '$Actual'."
    }
}

function Read-JsonLines([string]$Path) {
    $items = New-Object 'System.Collections.Generic.List[object]'
    foreach ($line in [System.IO.File]::ReadLines((Resolve-Path -LiteralPath $Path).Path)) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        try {
            $items.Add(($line | ConvertFrom-Json -ErrorAction Stop))
        } catch {
            throw "Invalid JSONL in '$Path': $($_.Exception.Message)"
        }
    }
    return $items.ToArray()
}

$repo = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($DatasetPath)) {
    $DatasetPath = Join-Path $repo 'commerce-runtime\testdata\s11\agentpay-benchmark\scenarios-recovery-v1.jsonl'
}
$localRoot = (Resolve-Path -LiteralPath (Join-Path $repo '.local-run')).Path.TrimEnd('\') + '\'
$runResolved = (Resolve-Path -LiteralPath $RunDir).Path
if (-not $runResolved.StartsWith($localRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'RunDir must resolve inside the repository .local-run directory.'
}

$datasetResolved = (Resolve-Path -LiteralPath $DatasetPath).Path
$datasetRoot = (Resolve-Path -LiteralPath (Join-Path $repo 'commerce-runtime\testdata\s11\agentpay-benchmark')).Path.TrimEnd('\') + '\'
if (-not $datasetResolved.StartsWith($datasetRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'DatasetPath must resolve under commerce-runtime/testdata/s11/agentpay-benchmark.'
}

$manifestPath = Join-Path $runResolved 'manifest.json'
$metricsPath = Join-Path $runResolved 'metrics.json'
$resultsPath = Join-Path $runResolved 'episode_results.jsonl'
foreach ($path in @($manifestPath, $metricsPath, $resultsPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required benchmark artifact is missing: $path"
    }
    $item = Get-Item -LiteralPath $path -Force
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Benchmark artifact must not be a reparse point: $path"
    }
}

$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
$metrics = Get-Content -LiteralPath $metricsPath -Raw | ConvertFrom-Json
$rows = @(Read-JsonLines $resultsPath)
$scenarios = @(Read-JsonLines $datasetResolved)
$profileKinds = @($scenarios | ForEach-Object { [string]$_.failure.kind } | Sort-Object -Unique)
$v1Kinds = @('delivery_invalid', 'merchant_transient')
$v2Kinds = @('delivery_invalid', 'merchant_transient', 'payment_transient')
$profileSet = $profileKinds -join ','
if ($profileSet -ne ($v1Kinds -join ',') -and $profileSet -ne ($v2Kinds -join ',')) {
    throw "Dataset must match the exact recovery-v1 or recovery-v2 profile set; got '$profileSet'."
}
$datasetVersion = if ($profileSet -eq ($v2Kinds -join ',')) { 'v2' } else { 'v1' }

$datasetHash = 'sha256:' + (Get-FileHash -LiteralPath $datasetResolved -Algorithm SHA256).Hash.ToLowerInvariant()
Assert-Equal $manifest.dataset_sha256 $datasetHash 'Manifest dataset SHA-256'
Assert-Equal $manifest.dataset_file ([IO.Path]::GetFileName($datasetResolved)) 'Manifest dataset filename'
if ([string]$manifest.source_revision -notmatch '^[0-9a-fA-F]{40,64}$') {
    throw 'Manifest is missing a full Git base revision.'
}
if ([string]$manifest.source_modified -notin @('true', 'false')) {
    throw 'Manifest must declare whether the source working tree was modified.'
}
if ([bool]$manifest.external_network_calls) {
    throw 'This local recovery suite must not make external network calls.'
}
if ([string]$manifest.merchant_payment_adapter -notmatch 'deterministic local mocks' -or
    [string]$manifest.runtime_per_trial -notmatch 'fresh.*isolated in-memory repository') {
    throw 'Manifest does not declare the expected isolated deterministic harness.'
}

$expectedByKind = @{
    merchant_transient = @{ task_id = 'merchant-transient'; trials = 25; injections = 2; business_recovery = 0 }
    delivery_invalid   = @{ task_id = 'delivery-invalid'; trials = 25; injections = 1; business_recovery = 25 }
    payment_transient  = @{ task_id = 'payment-response-loss'; trials = 25; injections = 1; business_recovery = 0 }
}
$taskByID = @{}
foreach ($scenario in $scenarios) {
    $kind = [string]$scenario.failure.kind
    if (-not $expectedByKind.ContainsKey($kind) -or $taskByID.ContainsKey([string]$scenario.task_id)) {
        throw "Unexpected or duplicate recovery scenario '$kind' / '$($scenario.task_id)'."
    }
    $taskByID[[string]$scenario.task_id] = $scenario
}
if ($taskByID.Count -ne $profileKinds.Count) { throw 'Recovery dataset has a duplicate or missing fault profile.' }
$expectedTrialCount = 0
$expectedBusinessRecoveryCount = 0
foreach ($kind in $profileKinds) {
    $expectedTrialCount += $expectedByKind[$kind].trials
    $expectedBusinessRecoveryCount += $expectedByKind[$kind].business_recovery
}

$requestIDs = @{}
$isolationIDs = @{}
$groupResults = @()
foreach ($kind in $profileKinds) {
    $expected = $expectedByKind[$kind]
    $task = $taskByID[$expected.task_id]
    Assert-Equal $task.failure.rate_percent 100 "$kind injection rate"
    Assert-Equal $task.failure.repeat $expected.injections "$kind configured injection repeat"
    Assert-Equal $task.expected_terminal 'FULFILLED' "$kind expected terminal"
    Assert-Equal $task.expected_payment_intents 1 "$kind expected PaymentIntent count"
    Assert-Equal $task.expected_settlement_count 1 "$kind expected settlement count"

    $group = @($rows | Where-Object { $_.failure.kind -eq $kind })
    Assert-Equal $group.Count $expected.trials "$kind trial count"
    $injectionCounts = @()
    $businessRecoveryCount = 0
    foreach ($row in $group) {
        if ([string]$row.task_id -ne $expected.task_id) { throw "$kind row has unexpected task_id '$($row.task_id)'." }
        $collectionError = $row.PSObject.Properties['collection_error']
        if ($null -ne $collectionError -and [string]$collectionError.Value -ne '') {
            throw "$($row.case_id) has a collection error."
        }
        if (-not $row.grade.passed) { throw "$($row.case_id) failed the deterministic grader." }
        if (-not $row.failure.configured -or -not $row.failure.triggered -or -not $row.failure.applied) {
            throw "$($row.case_id) did not prove an applied fault."
        }
        Assert-Equal $row.failure.injection_count $expected.injections "$($row.case_id) injection count"
        $injectionCounts += [int]$row.failure.injection_count

        Assert-Equal $row.trace.episode.state 'FULFILLED' "$($row.case_id) terminal state"
        Assert-Equal $row.trace.episode.selected_merchant_did $task.expected_merchant_did "$($row.case_id) selected merchant"
        Assert-Equal $row.trace.episode.selected_capability_id $task.expected_capability_id "$($row.case_id) selected capability"
        if (-not $row.trace.validation.valid) { throw "$($row.case_id) has invalid delivery evidence." }
        Assert-Equal $row.trace.artifact.body_hash $task.expected_artifact_body_hash "$($row.case_id) artifact hash"

        $paymentIntents = @($row.trace.payment_intents)
        Assert-Equal $paymentIntents.Count 1 "$($row.case_id) PaymentIntent count"
        $settlements = @($row.trace.ledger | Where-Object { $_.type -eq 'PAYMENT_SETTLED' })
        Assert-Equal $settlements.Count 1 "$($row.case_id) settlement count"
        Assert-Equal $row.trace.artifact.payment_intent_id $paymentIntents[0].intent_id "$($row.case_id) artifact PaymentIntent identity"

        if ($kind -eq 'payment_transient') {
            Assert-Equal $paymentIntents[0].status 'CONFIRMED' "$($row.case_id) reconciled PaymentIntent status"
            $transportKinds = @($row.trace.payment_transport_traces | ForEach-Object { [string]$_.kind } | Sort-Object)
            Assert-Equal ($transportKinds -join ',') 'INITIAL_SUBMIT,STATUS_QUERY' "$($row.case_id) submit/status-query recovery path"
            Assert-Equal $paymentIntents[0].tx_id $settlements[0].tx_id "$($row.case_id) reconciled transaction identity"
        }

        $passedRecoveryAssertions = @($row.grade.assertions | Where-Object { $_.name -eq 'must_enter_recovery' -and $_.passed })
        if ($kind -eq 'delivery_invalid') {
            Assert-Equal $passedRecoveryAssertions.Count 1 "$($row.case_id) business recovery assertion"
            $businessRecoveryCount++
        } elseif ($passedRecoveryAssertions.Count -ne 0) {
            throw "$($row.case_id) unexpectedly claims a business recovery assertion."
        }

        $requestID = [string]$row.request_id
        $isolationID = [string]$row.trial_isolation_id
        if (-not $requestID -or $requestIDs.ContainsKey($requestID)) { throw "Missing or duplicate request_id '$requestID'." }
        if (-not $isolationID -or $isolationIDs.ContainsKey($isolationID)) { throw "Missing or duplicate trial_isolation_id '$isolationID'." }
        $requestIDs[$requestID] = $true
        $isolationIDs[$isolationID] = $true
    }
    if (($injectionCounts | Measure-Object -Minimum).Minimum -ne $expected.injections -or
        ($injectionCounts | Measure-Object -Maximum).Maximum -ne $expected.injections) {
        throw "$kind trials do not all have the expected injected-fault count."
    }
    Assert-Equal $businessRecoveryCount $expected.business_recovery "$kind business recovery count"

    $faultMetric = $metrics.fault_coverage.PSObject.Properties[$kind].Value
    Assert-Equal $faultMetric.requested $expected.trials "$kind metrics requested count"
    Assert-Equal $faultMetric.configured $expected.trials "$kind metrics configured count"
    Assert-Equal $faultMetric.triggered $expected.trials "$kind metrics triggered count"
    Assert-Equal $faultMetric.task_recovered $expected.trials "$kind metrics recovered count"
    Assert-Equal $faultMetric.business_recovery_entered $expected.business_recovery "$kind metrics business recovery count"
    $groupResults += [pscustomobject]@{
        fault = $kind
        trials = $group.Count
        injections_per_trial = $expected.injections
        all_triggered_and_recovered = $true
        business_recovery_trials = $businessRecoveryCount
    }
}

Assert-Equal $rows.Count $expectedTrialCount 'Total raw trial count'
Assert-Equal $metrics.total_episodes $expectedTrialCount 'Metrics episode count'
Assert-Equal $metrics.completed_episodes $expectedTrialCount 'Metrics completed count'
Assert-Equal $metrics.successful_episodes $expectedTrialCount 'Metrics successful count'
Assert-Equal $metrics.fault_triggered $expectedTrialCount 'Metrics triggered fault count'
Assert-Equal $metrics.fault_task_recovered $expectedTrialCount 'Metrics recovered fault count'
Assert-Equal $metrics.business_recovery_entered $expectedBusinessRecoveryCount 'Metrics business recovery count'
Assert-Equal $metrics.recovery_episodes $expectedBusinessRecoveryCount 'Metrics recovery episode count'
Assert-Equal $metrics.recovery_successes $expectedBusinessRecoveryCount 'Metrics successful recovery count'
Assert-Equal $metrics.duplicate_settlement_count 0 'Metrics duplicate settlement count'
Assert-Equal $metrics.business_payment_resubmission_count 0 'Metrics payment resubmission count'
Assert-Equal $metrics.evidence.submitted_trials $expectedTrialCount 'Metrics submitted trial count'
Assert-Equal $metrics.evidence.valid_trials $expectedTrialCount 'Metrics valid trial count'
Assert-Equal $metrics.evidence.requested_fault_cases $expectedTrialCount 'Metrics requested fault case count'
Assert-Equal $metrics.evidence.applied_fault_cases $expectedTrialCount 'Metrics applied fault case count'
Assert-Equal $metrics.evidence.collection_error_count 0 'Metrics collection error count'
Assert-Equal $requestIDs.Count $expectedTrialCount 'Unique request ID count'
Assert-Equal $isolationIDs.Count $expectedTrialCount 'Unique trial isolation ID count'
if ([math]::Abs([double]$metrics.fault_recovery_success_rate - 1.0) -gt 1e-9 -or
    [math]::Abs([double]$metrics.business_recovery_success_rate - 1.0) -gt 1e-9) {
    throw 'Published recovery rates do not match the independently audited trial rows.'
}

[ordered]@{
    audit = 'AGENTPAY_LOCAL_RECOVERY_REPLAY'
    status = 'PASS'
    run_dir = $runResolved
    source_revision = [string]$manifest.source_revision
    source_modified = [string]$manifest.source_modified
    dataset_version = $datasetVersion
    dataset_sha256 = $datasetHash
    trial_count = $rows.Count
    fault_recovery_success = "$($expectedTrialCount)/$($expectedTrialCount)"
    business_recovery_success = "$($expectedBusinessRecoveryCount)/$($expectedBusinessRecoveryCount)"
    duplicate_settlements = [int]$metrics.duplicate_settlement_count
    payment_resubmissions = [int]$metrics.business_payment_resubmission_count
    collection_errors = [int]$metrics.evidence.collection_error_count
    episode_p95_ms = [double]$metrics.episode_latency_p95_ms
    fault_profiles = $groupResults
    artifact_sha256 = [ordered]@{
        manifest = 'sha256:' + (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
        metrics = 'sha256:' + (Get-FileHash -LiteralPath $metricsPath -Algorithm SHA256).Hash.ToLowerInvariant()
        episode_results = 'sha256:' + (Get-FileHash -LiteralPath $resultsPath -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    scope = if ($datasetVersion -eq 'v2') { 'Three fixed deterministic live-local mock profiles, 25 isolated Runtime trials each; payment response loss is resolved by a mock status query; not real payment or unique natural-language task quality.' } else { 'Two fixed deterministic live-local mock profiles, 25 isolated Runtime trials each; not real payment or unique natural-language task quality.' }
} | ConvertTo-Json -Depth 6
