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

function Assert-PassedAssertion([object]$Result, [string]$Name) {
    $assertion = @($Result.grade.assertions | Where-Object { $_.name -eq $Name })
    if ($assertion.Count -ne 1 -or -not [bool]$assertion[0].passed) {
        throw "$($Result.case_id) did not pass required evidence assertion '$Name'."
    }
}

$repo = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($DatasetPath)) {
    $DatasetPath = Join-Path $repo 'commerce-runtime\testdata\s11\agentpay-benchmark\scenarios-paired-recovery-v1.jsonl'
}
$localRoot = (Resolve-Path -LiteralPath (Join-Path $repo '.local-run')).Path.TrimEnd('\') + '\'
$runResolved = (Resolve-Path -LiteralPath $RunDir).Path
if (-not $runResolved.StartsWith($localRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'RunDir must resolve inside the repository .local-run directory.'
}
$datasetRoot = (Resolve-Path -LiteralPath (Join-Path $repo 'commerce-runtime\testdata\s11\agentpay-benchmark')).Path.TrimEnd('\') + '\'
$datasetResolved = (Resolve-Path -LiteralPath $DatasetPath).Path
if (-not $datasetResolved.StartsWith($datasetRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'DatasetPath must resolve under commerce-runtime/testdata/s11/agentpay-benchmark.'
}

$manifestPath = Join-Path $runResolved 'manifest.json'
$metricsPath = Join-Path $runResolved 'metrics.json'
$resultsPath = Join-Path $runResolved 'episode_results.jsonl'
foreach ($path in @($manifestPath, $metricsPath, $resultsPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing benchmark artifact: $path" }
    if (((Get-Item -LiteralPath $path -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Benchmark artifact must not be a reparse point: $path"
    }
}

$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
$metrics = Get-Content -LiteralPath $metricsPath -Raw | ConvertFrom-Json
$rows = @(Read-JsonLines $resultsPath)
$scenarios = @(Read-JsonLines $datasetResolved)
Assert-Equal $scenarios.Count 2 'Dataset arm count'
$controlScenario = @($scenarios | Where-Object recovery_provider -eq 'baseline_stop')
$ruleScenario = @($scenarios | Where-Object recovery_provider -eq 'rule')
Assert-Equal $controlScenario.Count 1 'Stop-control scenario count'
Assert-Equal $ruleScenario.Count 1 'Rule-treatment scenario count'
Assert-Equal $controlScenario[0].task_id $ruleScenario[0].task_id 'Paired task ID'
Assert-Equal $controlScenario[0].seed $ruleScenario[0].seed 'Paired seed'
Assert-Equal $controlScenario[0].failure.kind 'delivery_invalid' 'Control fault kind'
Assert-Equal $ruleScenario[0].failure.kind 'delivery_invalid' 'Treatment fault kind'
Assert-Equal $controlScenario[0].failure.rate_percent 100 'Control fault rate'
Assert-Equal $ruleScenario[0].failure.rate_percent 100 'Treatment fault rate'
Assert-Equal $controlScenario[0].failure.repeat 1 'Control fault repeat'
Assert-Equal $ruleScenario[0].failure.repeat 1 'Treatment fault repeat'
Assert-Equal $controlScenario[0].expected_terminal 'FULFILLED' 'Control buyer objective'
Assert-Equal $ruleScenario[0].expected_terminal 'FULFILLED' 'Treatment buyer objective'
$requestA = ($controlScenario[0].request | ConvertTo-Json -Depth 30 -Compress) -replace '"request_id":"[^"]*"', '"request_id":"<arm>"'
$requestB = ($ruleScenario[0].request | ConvertTo-Json -Depth 30 -Compress) -replace '"request_id":"[^"]*"', '"request_id":"<arm>"'
Assert-Equal $requestA $requestB 'Paired request content excluding arm-specific request_id'

$datasetHash = 'sha256:' + (Get-FileHash -LiteralPath $datasetResolved -Algorithm SHA256).Hash.ToLowerInvariant()
Assert-Equal $manifest.dataset_sha256 $datasetHash 'Manifest dataset SHA-256'
Assert-Equal $manifest.dataset_file ([IO.Path]::GetFileName($datasetResolved)) 'Manifest dataset filename'
if ([bool]$manifest.external_network_calls) { throw 'Paired local recovery suite must make no external network calls.' }
if ([string]$manifest.runtime_per_trial -notmatch 'fresh.*isolated in-memory repository') {
    throw 'Manifest does not prove a fresh isolated in-memory Runtime per trial.'
}
$trials = [int]$manifest.independent_trials_each
if ($trials -lt 1) { throw 'Manifest independent_trials_each must be positive.' }
Assert-Equal $manifest.distinct_task_profiles 1 'Distinct task profile count'
Assert-Equal $manifest.expanded_episode_count (2 * $trials) 'Expanded paired episode count'
Assert-Equal $rows.Count (2 * $trials) 'Raw episode row count'
Assert-Equal $metrics.total_episodes (2 * $trials) 'Metrics episode count'

$controlCompleted = 0
$ruleCompleted = 0
$triggeredCount = 0
$duplicateSettlementCount = 0
$controlUncompensatedSettlements = 0
$paymentIntentCount = 0
$settlementCount = 0
$requestIDs = @{}
$isolationIDs = @{}
$pairRows = @()
for ($trial = 1; $trial -le $trials; $trial++) {
    $control = @($rows | Where-Object { $_.recovery_provider -eq 'baseline_stop' -and [int]$_.trial_index -eq $trial })
    $treatment = @($rows | Where-Object { $_.recovery_provider -eq 'rule' -and [int]$_.trial_index -eq $trial })
    Assert-Equal $control.Count 1 "Trial $trial control row count"
    Assert-Equal $treatment.Count 1 "Trial $trial rule row count"
    $control = $control[0]
    $treatment = $treatment[0]
    foreach ($row in @($control, $treatment)) {
        Assert-Equal $row.task_id $controlScenario[0].task_id "$($row.case_id) task ID"
        Assert-Equal $row.failure.kind 'delivery_invalid' "$($row.case_id) fault kind"
        Assert-Equal $row.failure.injection_count 1 "$($row.case_id) injection count"
        if (-not $row.failure.configured -or -not $row.failure.triggered -or -not $row.failure.applied) {
            throw "$($row.case_id) does not prove the invalid-delivery fault was applied."
        }
        $collectionErrorProperty = $row.PSObject.Properties['collection_error']
        $collectionError = if ($null -eq $collectionErrorProperty) { '' } else { [string]$collectionErrorProperty.Value }
        if ($collectionError -ne '') { throw "$($row.case_id) has collection_error '$collectionError'" }
        if (-not $row.trial_isolation_id -or $isolationIDs.ContainsKey([string]$row.trial_isolation_id)) {
            throw "Missing or duplicate trial isolation ID '$($row.trial_isolation_id)'."
        }
        if (-not $row.request_id -or $requestIDs.ContainsKey([string]$row.request_id)) {
            throw "Missing or duplicate request ID '$($row.request_id)'."
        }
        $isolationIDs[[string]$row.trial_isolation_id] = $true
        $requestIDs[[string]$row.request_id] = $true

        $intents = @($row.trace.payment_intents)
        $settlements = @($row.trace.ledger | Where-Object { $_.type -eq 'PAYMENT_SETTLED' })
        $compensations = @($row.trace.ledger | Where-Object { $_.type -in @('REFUND_CONFIRMED', 'COMPENSATION_RECORDED') })
        Assert-Equal $intents.Count 1 "$($row.case_id) PaymentIntent count"
        Assert-Equal $settlements.Count 1 "$($row.case_id) settlement count"
        Assert-PassedAssertion $row 'duplicate_settlement_count'
        Assert-PassedAssertion $row 'unsafe_side_effect_count'
        Assert-PassedAssertion $row 'expected_payment_intents'
        Assert-PassedAssertion $row 'expected_settlement_count'
        Assert-PassedAssertion $row 'must_not_create_second_payment'
        Assert-PassedAssertion $row 'payment_confirmed'
        Assert-PassedAssertion $row 'payment_settled'
        Assert-PassedAssertion $row 'budget_ledger_consistency'
        $paymentIntentCount += $intents.Count
        $settlementCount += $settlements.Count
        $triggeredCount++
        if ($settlements.Count -gt 1) { $duplicateSettlementCount += ($settlements.Count - 1) }

        if ($row.recovery_provider -eq 'baseline_stop') {
            if ([string]$row.trace.episode.state -eq 'FULFILLED') { $controlCompleted++ }
            Assert-Equal $row.trace.episode.state 'FAILED' "$($row.case_id) observed stop-control state"
            if ([bool]$row.grade.passed) { throw "$($row.case_id) incorrectly grades the unfulfilled task as completed." }
            $failedAssertions = @($row.grade.assertions | Where-Object { $_.required -and -not $_.passed } | ForEach-Object { [string]$_.name })
            $allowedFailures = @('terminal_expected', 'fulfilled_artifact_present', 'fulfilled_validation_present', 'validation_valid')
            foreach ($name in $failedAssertions) {
                if ($name -notin $allowedFailures) { throw "$($row.case_id) has unexpected failed assertion '$name'." }
            }
            if ($failedAssertions.Count -eq 0) { throw "$($row.case_id) did not record the expected task-completion failure." }
            if ($compensations.Count -eq 0) { $controlUncompensatedSettlements++ }
        } else {
            Assert-Equal $row.trace.episode.state 'FULFILLED' "$($row.case_id) observed rule state"
            if (-not [bool]$row.grade.passed) { throw "$($row.case_id) failed task or safety grading." }
            if ($null -eq $row.trace.validation -or -not [bool]$row.trace.validation.valid) {
                throw "$($row.case_id) lacks valid delivery evidence."
            }
            if ($null -eq $row.trace.artifact -or [string]::IsNullOrWhiteSpace([string]$row.trace.artifact.delivery_id)) {
                throw "$($row.case_id) lacks a delivered artifact."
            }
            $ruleCompleted++
        }
    }
    $pairRows += [pscustomobject]@{
        task_id = $control.task_id
        trial_index = $trial
        same_seed = ([int64]$control.seed -eq [int64]$treatment.seed)
        stop_control_state = [string]$control.trace.episode.state
        rule_state = [string]$treatment.trace.episode.state
        stop_control_grade_passed = [bool]$control.grade.passed
        rule_grade_passed = [bool]$treatment.grade.passed
    }
    Assert-Equal $pairRows[-1].same_seed $true "Trial $trial seed pairing"
}

Assert-Equal $triggeredCount (2 * $trials) 'Applied fault count'
Assert-Equal $requestIDs.Count (2 * $trials) 'Unique request ID count'
Assert-Equal $isolationIDs.Count (2 * $trials) 'Unique isolation ID count'
Assert-Equal $controlCompleted 0 'Stop-control fulfilled count'
Assert-Equal $ruleCompleted $trials 'Rule fulfilled count'
Assert-Equal $paymentIntentCount (2 * $trials) 'Total PaymentIntent count'
Assert-Equal $settlementCount (2 * $trials) 'Total settlement count'
Assert-Equal $duplicateSettlementCount 0 'Duplicate settlement count'
Assert-Equal $metrics.duplicate_settlement_count 0 'Aggregated duplicate settlement count'
Assert-Equal $metrics.business_payment_resubmission_count 0 'Payment resubmission count'
Assert-Equal $metrics.evidence.collection_error_count 0 'Collection error count'
Assert-Equal $metrics.llm_proposals 0 'LLM proposal count for deterministic policy arms'

$audit = [ordered]@{
    audit = 'AGENTPAY_PAIRED_RECOVERY_POLICY_ABLATION'
    status = 'PASS'
    run_dir = $runResolved
    dataset_sha256 = $datasetHash
    task_id = [string]$controlScenario[0].task_id
    paired_trials = $trials
    stop_without_retry_fulfilled = "0/$trials"
    rule_recovery_fulfilled = "$ruleCompleted/$trials"
    completion_delta_percentage_points = [math]::Round((100.0 * $ruleCompleted / $trials), 2)
    triggered_faults = $triggeredCount
    payment_intents = $paymentIntentCount
    settlements = $settlementCount
    duplicate_settlements = $duplicateSettlementCount
    uncompensated_mock_stop_control_settlements = $controlUncompensatedSettlements
    collection_errors = [int]$metrics.evidence.collection_error_count
    descriptive_only = $true
    resume_claim_eligible = $false
    trial_pairs = $pairRows
    artifact_sha256 = [ordered]@{
        manifest = 'sha256:' + (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
        metrics = 'sha256:' + (Get-FileHash -LiteralPath $metricsPath -Algorithm SHA256).Hash.ToLowerInvariant()
        episode_results = 'sha256:' + (Get-FileHash -LiteralPath $resultsPath -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    scope = 'One synthetic paid task profile repeated in independent in-memory Runtime trials; stop-without-retry control is expected to miss fulfillment. Payments are local mocks. Counts are descriptive for this fixed fault profile, not a marketplace or production reliability estimate.'
}
$auditPath = Join-Path $runResolved 'audit-summary.json'
[System.IO.File]::WriteAllText($auditPath, ($audit | ConvertTo-Json -Depth 8) + "`n", [System.Text.UTF8Encoding]::new($false))
$audit | ConvertTo-Json -Depth 8
