param(
    [Parameter(Mandatory = $true)] [string] $TasksPath,
    [Parameter(Mandatory = $true)] [string] $TrialsPath,
    [Parameter(Mandatory = $true)] [string] $ManifestPath,
    [Parameter(Mandatory = $true)] [string] $MetricsPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Read-JsonLines([string] $Path) {
    $items = New-Object 'System.Collections.Generic.List[object]'
    foreach ($line in [System.IO.File]::ReadLines((Resolve-Path -LiteralPath $Path).Path)) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        $items.Add(($line | ConvertFrom-Json))
    }
    return $items.ToArray()
}

function Get-Quantile([object[]] $Values, [double] $Quantile) {
    $sorted = @($Values | Sort-Object)
    if ($sorted.Count -eq 0) { return $null }
    $index = [int][math]::Ceiling($Quantile * $sorted.Count) - 1
    if ($index -lt 0) { $index = 0 }
    return [int]$sorted[$index]
}

function Get-Counts([object[]] $Values) {
    $counts = [ordered]@{}
    foreach ($group in ($Values | Group-Object | Sort-Object Name)) {
        $counts[[string]$group.Name] = $group.Count
    }
    return $counts
}

foreach ($path in @($TasksPath, $TrialsPath, $ManifestPath, $MetricsPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required audit input does not exist: $path"
    }
}

$tasks = @(Read-JsonLines $TasksPath)
$trials = @(Read-JsonLines $TrialsPath)
$manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
$metricsDocument = Get-Content -LiteralPath $MetricsPath -Raw | ConvertFrom-Json
$metrics = $metricsDocument.metrics
$taskFileHash = 'sha256:' + (Get-FileHash -LiteralPath $TasksPath -Algorithm SHA256).Hash.ToLowerInvariant()

if ($taskFileHash -ne [string]$manifest.hashes.tasks_sha256 -or
    $taskFileHash -ne [string]$metrics.hashes.tasks_sha256) {
    throw 'Task file SHA-256 does not match both run manifest and metrics.'
}
foreach ($hashName in @('catalog_sha256', 'inventory_sha256', 'tasks_sha256', 'source_manifest_sha256')) {
    if ([string]$manifest.hashes.$hashName -ne [string]$metrics.hashes.$hashName) {
        throw "Manifest/metrics hash mismatch for '$hashName'."
    }
}

if ($tasks.Count -eq 0 -or $tasks.Count -ne $trials.Count -or
    $tasks.Count -ne [int]$manifest.task_count -or $tasks.Count -ne [int]$metrics.task_count -or
    [string]$manifest.run_id -ne [string]$metricsDocument.run_id) {
    throw 'Task, trial, manifest, and metrics dimensions/run IDs do not agree.'
}

$taskById = @{}
foreach ($task in $tasks) {
    $id = [string]$task.task_id
    if ([string]::IsNullOrWhiteSpace($id) -or $taskById.ContainsKey($id)) {
        throw "Task IDs must be non-empty and unique: '$id'"
    }
    $taskById[$id] = $task
}

$trialById = @{}
foreach ($trial in $trials) {
    $id = [string]$trial.task_id
    if ([string]::IsNullOrWhiteSpace($id) -or $trialById.ContainsKey($id) -or -not $taskById.ContainsKey($id)) {
        throw "Trial IDs must be unique and match a task: '$id'"
    }
    if ([string]$trial.expected_disposition -ne [string]$taskById[$id].expected_disposition) {
        throw "Task/trial expected-disposition mismatch for '$id'."
    }
    $trialById[$id] = $trial
}
if ($trialById.Count -ne $taskById.Count) {
    throw 'Task and trial ID sets do not match.'
}

$planTasks = @($tasks | Where-Object { $_.expected_disposition -eq 'PLAN' })
$noPlanTasks = @($tasks | Where-Object { $_.expected_disposition -eq 'NO_PLAN' })
$clarifyTasks = @($tasks | Where-Object { $_.expected_disposition -eq 'CLARIFY' })
$planTrials = @($trials | Where-Object { $_.expected_disposition -eq 'PLAN' })

$hardAttributeCounts = @(
    foreach ($task in $planTasks) {
        $attributes = $task.gold_intent.required_attributes
        if ($null -eq $attributes) { 0 }
        else { @($attributes.PSObject.Properties | Where-Object { $_.MemberType -eq 'NoteProperty' }).Count }
    }
)
$literalItemCount = @($planTasks | Where-Object {
    [string]$_.gold_intent.item -and $_.prompt.Contains([string]$_.gold_intent.item)
}).Count
$literalFormatCount = @($planTasks | Where-Object {
    [string]$_.gold_intent.required_format -and $_.prompt.Contains([string]$_.gold_intent.required_format)
}).Count
$literalStyleCount = @($planTasks | Where-Object {
    [string]$_.gold_intent.preferred_style -and $_.prompt.Contains([string]$_.gold_intent.preferred_style)
}).Count
$attributeClauseCount = @($planTasks | Where-Object {
    $_.prompt -match '必须同时满足的硬性商品属性'
}).Count

$discoveryCounts = @($planTrials | ForEach-Object { [int]$_.discovery_candidate_count })
$feasibleCounts = @($planTrials | ForEach-Object { [int]$_.feasible_candidate_count })
$noPlanScenarios = Get-Counts @($noPlanTasks | ForEach-Object { [string]$_.scenario })
$clarificationPatterns = Get-Counts @($clarifyTasks | ForEach-Object {
    @($_.required_missing_fields | ForEach-Object { [string]$_ } | Sort-Object) -join ','
})
$dispositionCounts = Get-Counts @($tasks | ForEach-Object { [string]$_.expected_disposition })
$baselineValidCount = @($planTrials | Where-Object { $_.baseline_valid_plan }).Count
$validPlanCount = @($planTrials | Where-Object { $_.valid_plan }).Count
$taskHitCount = @($planTrials | Where-Object { $_.discovery_hit_any_gold_at_k }).Count
$completeIntentTrials = @($trials | Where-Object { $_.expected_disposition -in @('PLAN', 'NO_PLAN') })
$exactIntentCount = @($completeIntentTrials | Where-Object { $_.exact_intent_match }).Count
$noPlanCorrectCount = @($trials | Where-Object { $_.expected_disposition -eq 'NO_PLAN' -and $_.correct_decision }).Count
$clarifyCorrectCount = @($trials | Where-Object { $_.expected_disposition -eq 'CLARIFY' -and $_.correct_decision }).Count
$clarifyTrials = @($trials | Where-Object { $_.expected_disposition -eq 'CLARIFY' })
$clarificationQuestionCount = @($clarifyTrials | Where-Object {
    $property = $_.PSObject.Properties['clarification_question_provided']
    $null -ne $property -and [bool]$property.Value
}).Count
$clarificationQuestionFieldMissingCount = @($clarifyTrials | Where-Object {
    $null -eq $_.PSObject.Properties['clarification_question_provided']
}).Count
$clarificationQuestionCueFieldMissingCount = @($clarifyTrials | Where-Object {
    $null -eq $_.PSObject.Properties['clarification_question_targets_missing_fields']
}).Count
$clarificationQuestionCueCount = @($clarifyTrials | Where-Object {
    $property = $_.PSObject.Properties['clarification_question_targets_missing_fields']
    $null -ne $property -and [bool]$property.Value
}).Count
$parseFailureCount = @($trials | Where-Object { -not $_.parse_valid }).Count

if ($planTasks.Count -ne [int]$metrics.plan_tasks -or
    $baselineValidCount -ne [int]$metrics.plan_comparison.baseline_valid_plan_rate.numerator -or
    $validPlanCount -ne [int]$metrics.valid_plan_rate.numerator -or
    $taskHitCount -ne [int]$metrics.plan_task_hit_at_k.numerator -or
    $completeIntentTrials.Count -ne [int]$metrics.exact_intent_rate_on_complete_intents.denominator -or
    $exactIntentCount -ne [int]$metrics.exact_intent_rate_on_complete_intents.numerator -or
    $noPlanCorrectCount -ne [int]$metrics.correct_no_plan_rate.numerator -or
    $clarifyCorrectCount -ne [int]$metrics.correct_clarification_rate.numerator -or
    $clarifyTasks.Count -ne [int]$metrics.correct_clarification_rate.denominator -or
    $parseFailureCount -ne [int]$metrics.parse_failures) {
    throw 'Trial-derived counts do not reconcile with the published metrics.'
}

$clarificationQuestionProperty = $metrics.PSObject.Properties['clarification_question_provided_rate']
$clarificationCueProperty = $metrics.PSObject.Properties['clarification_question_missing_field_cue_rate']
$clarificationQuestionMetric = if ($null -ne $clarificationQuestionProperty) { $clarificationQuestionProperty.Value } else { $null }
$clarificationCueMetric = if ($null -ne $clarificationCueProperty) { $clarificationCueProperty.Value } else { $null }
$hasClarificationQuestionMetrics = $null -ne $clarificationQuestionProperty
$hasClarificationCueMetric = $null -ne $clarificationCueProperty
if ($metrics.schema_version -eq 'stablepay.agent-shopping-challenge.v6' -and
    (-not $hasClarificationQuestionMetrics -or -not $hasClarificationCueMetric -or
     $clarificationQuestionFieldMissingCount -gt 0 -or $clarificationQuestionCueFieldMissingCount -gt 0)) {
    throw 'Challenge evaluator v6 metrics are missing clarification-question audit fields.'
}
if ($hasClarificationQuestionMetrics -and
    ($clarificationQuestionCount -ne [int]$clarificationQuestionMetric.numerator -or
     $clarifyTasks.Count -ne [int]$clarificationQuestionMetric.denominator)) {
    throw 'Trial-derived nonempty clarification-question counts do not reconcile with metrics.'
}
if ($hasClarificationCueMetric -and
    ($clarificationQuestionCueCount -ne [int]$clarificationCueMetric.numerator -or
     $clarifyTasks.Count -ne [int]$clarificationCueMetric.denominator)) {
    throw 'Trial-derived missing-field cue counts do not reconcile with metrics.'
}

$report = [ordered]@{
    audit = 'READ_ONLY_AGENT_SHOPPING_TASK_DIFFICULTY'
    run_id = [string]$manifest.run_id
    run_mode = [string]$manifest.run_mode
    suite_status = [string]$manifest.suite_status
    api_calls_status = [string]$manifest.api_calls_status
    tasks_sha256 = [string]$manifest.hashes.tasks_sha256
    artifact_sha256 = [ordered]@{
        tasks = $taskFileHash
        trials = 'sha256:' + (Get-FileHash -LiteralPath $TrialsPath -Algorithm SHA256).Hash.ToLowerInvariant()
        manifest = 'sha256:' + (Get-FileHash -LiteralPath $ManifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
        metrics = 'sha256:' + (Get-FileHash -LiteralPath $MetricsPath -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    task_count = $tasks.Count
    disposition_counts = $dispositionCounts
    plan_prompt_surface = [ordered]@{
        canonical_item_literal = "$literalItemCount/$($planTasks.Count)"
        canonical_format_literal = "$literalFormatCount/$($planTasks.Count)"
        canonical_style_literal = "$literalStyleCount/$($planTasks.Count)"
        generated_hard_attribute_clause = "$attributeClauseCount/$($planTasks.Count)"
        hard_attribute_count_distribution = Get-Counts $hardAttributeCounts
    }
    no_plan_scenario_counts = $noPlanScenarios
    clarification_missing_field_patterns = $clarificationPatterns
    model_and_plan = [ordered]@{
        exact_intent = "$($metrics.exact_intent_rate_on_complete_intents.numerator)/$($metrics.exact_intent_rate_on_complete_intents.denominator)"
        baseline_valid_plans = "$($metrics.plan_comparison.baseline_valid_plan_rate.numerator)/$($metrics.plan_comparison.baseline_valid_plan_rate.denominator)"
        treatment_valid_plans = "$($metrics.valid_plan_rate.numerator)/$($metrics.valid_plan_rate.denominator)"
        feasible_gold_task_hit_at_k = "$($metrics.plan_task_hit_at_k.numerator)/$($metrics.plan_task_hit_at_k.denominator)"
        valid_plan_despite_no_raw_top_k_gold = @($planTrials | Where-Object {
            -not $_.discovery_hit_any_gold_at_k -and $_.valid_plan
        }).Count
        discovery_candidates_min_p50_p95_max = @(
            (Get-Quantile $discoveryCounts 0.0), (Get-Quantile $discoveryCounts 0.5),
            (Get-Quantile $discoveryCounts 0.95), (Get-Quantile $discoveryCounts 1.0)
        )
        post_filter_candidates_min_p50_p95_max = @(
            (Get-Quantile $feasibleCounts 0.0), (Get-Quantile $feasibleCounts 0.5),
            (Get-Quantile $feasibleCounts 0.95), (Get-Quantile $feasibleCounts 1.0)
        )
    }
    clarification_quality = [ordered]@{
        correct_decisions = "$($metrics.correct_clarification_rate.numerator)/$($metrics.correct_clarification_rate.denominator)"
        nonempty_questions = if ($hasClarificationQuestionMetrics) { "$($clarificationQuestionMetric.numerator)/$($clarificationQuestionMetric.denominator)" } else { 'UNAVAILABLE_LEGACY_SCHEMA' }
        missing_field_cue_proxy = if ($hasClarificationCueMetric) { "$($clarificationCueMetric.numerator)/$($clarificationCueMetric.denominator)" } else { 'UNAVAILABLE_LEGACY_SCHEMA' }
    }
    target_product_recall_status = [string]$metrics.target_product_recall_at_k.status
    independent_relevance_labeled_plan_tasks = [int]$metrics.target_product_recall_at_k.labeled_plan_task_count
}

$report | ConvertTo-Json -Depth 8
