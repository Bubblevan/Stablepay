param(
    [Parameter(Mandatory = $true)] [string] $TasksPath,
    [Parameter(Mandatory = $true)] [string] $OutDir,
    [string] $SplitSeed = 'stablepay-b2-holdout-v1',
    [ValidateRange(10, 40)] [int] $HoldoutPercent = 20
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Get-Sha256Bytes([byte[]] $Bytes) {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try { return -join ($sha.ComputeHash($Bytes) | ForEach-Object { $_.ToString('x2') }) }
    finally { $sha.Dispose() }
}

function Get-FileSha256([string] $Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-Decision([object] $Task) {
    return [string]$Task.expected_disposition
}

function Get-GroupKey([object] $Task) {
    $intent = $Task.gold_intent
    $decision = Get-Decision $Task
    if ($decision -eq 'CLARIFY') {
        $missing = @($Task.required_missing_fields | ForEach-Object { [string]$_ } | Sort-Object) -join '+'
        return 'CLARIFY|' + [string]$intent.category + '|' + $missing
    }
    if ($decision -notin @('PLAN', 'NO_PLAN')) {
        throw "Unsupported decision '$decision' in task '$($Task.task_id)'."
    }
    return 'PRODUCT|' + [string]$intent.category + '|' + [string]$intent.item + '|' + [string]$intent.required_format
}

function Write-Utf8NoBom([string] $Path, [string] $Text) {
    [System.IO.File]::WriteAllText($Path, $Text, [System.Text.UTF8Encoding]::new($false))
}

$suiteRoot = [System.IO.Path]::GetFullPath((Join-Path (Get-Location).Path '.local-run/agent-shopping/challenge-suites'))
$tasksFull = (Resolve-Path -LiteralPath $TasksPath).Path
$relativeTasks = [System.IO.Path]::GetRelativePath($suiteRoot, $tasksFull)
if ([System.IO.Path]::IsPathRooted($relativeTasks) -or $relativeTasks -eq '..' -or $relativeTasks.StartsWith('..' + [System.IO.Path]::DirectorySeparatorChar)) {
    throw 'Input tasks must remain under .local-run/agent-shopping/challenge-suites.'
}

$outFull = [System.IO.Path]::GetFullPath((Join-Path $suiteRoot $OutDir))
if ([System.IO.Path]::GetDirectoryName($outFull) -ne $suiteRoot -or $outFull -eq $suiteRoot) {
    throw 'Output must be a new direct child of .local-run/agent-shopping/challenge-suites.'
}
if (Test-Path -LiteralPath $outFull) { throw "Output already exists; refusing overwrite: $outFull" }

$lines = [System.IO.File]::ReadAllLines($tasksFull)
if ($lines.Count -ne 500) { throw "Expected exactly 500 input tasks; got $($lines.Count)." }
$records = [System.Collections.Generic.List[object]]::new()
$seenIds = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
$groupBuckets = @{}
$decisionCounts = @{}
$promptHashes = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)

foreach ($line in $lines) {
    if ([string]::IsNullOrWhiteSpace($line)) { throw 'Task JSONL contains an empty line.' }
    $task = $line | ConvertFrom-Json
    $id = [string]$task.task_id
    if ([string]::IsNullOrWhiteSpace($id) -or -not $seenIds.Add($id)) { throw "Task IDs must be non-empty and unique: '$id'." }
    if ([string]::IsNullOrWhiteSpace([string]$task.prompt)) { throw "Task '$id' has an empty prompt." }

    $decision = Get-Decision $task
    if (-not $decisionCounts.ContainsKey($decision)) { $decisionCounts[$decision] = 0 }
    $decisionCounts[$decision]++
    $group = Get-GroupKey $task
    if (-not $groupBuckets.ContainsKey($group)) {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes("$SplitSeed|$group")
        $digest = [System.Security.Cryptography.SHA256]::HashData($bytes)
        $bucket = (([int]$digest[0] * 256) + [int]$digest[1]) % 100
        $groupBuckets[$group] = $bucket
    }
    [void]$promptHashes.Add((Get-Sha256Bytes ([System.Text.Encoding]::UTF8.GetBytes([string]$task.prompt))))
    $records.Add([pscustomobject]@{ Line = $line; Group = $group; Bucket = [int]$groupBuckets[$group]; Decision = $decision })
}

$devLines = [System.Collections.Generic.List[string]]::new()
$holdoutLines = [System.Collections.Generic.List[string]]::new()
$devGroups = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
$holdoutGroups = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::Ordinal)
$devCounts = @{}
$holdoutCounts = @{}
foreach ($record in $records) {
    if ($record.Bucket -lt $HoldoutPercent) {
        $holdoutLines.Add($record.Line)
        [void]$holdoutGroups.Add($record.Group)
        if (-not $holdoutCounts.ContainsKey($record.Decision)) { $holdoutCounts[$record.Decision] = 0 }
        $holdoutCounts[$record.Decision]++
    } else {
        $devLines.Add($record.Line)
        [void]$devGroups.Add($record.Group)
        if (-not $devCounts.ContainsKey($record.Decision)) { $devCounts[$record.Decision] = 0 }
        $devCounts[$record.Decision]++
    }
}

if ($holdoutLines.Count -lt 50 -or $holdoutLines.Count -gt 150) {
    throw "Grouped holdout has $($holdoutLines.Count) tasks; expected 50–150. Choose another split seed rather than splitting product families."
}
$requiredDecisions = @('PLAN', 'NO_PLAN', 'CLARIFY')
if (@($requiredDecisions | Where-Object { -not $holdoutCounts.ContainsKey($_) }).Count -gt 0) {
    throw 'Grouped holdout must contain PLAN, NO_PLAN, and CLARIFY tasks.'
}
foreach ($group in $holdoutGroups) {
    if ($devGroups.Contains($group)) { throw "Semantic group leaked across the split: $group" }
}

[void][System.IO.Directory]::CreateDirectory($outFull)
$devPath = Join-Path $outFull 'development.tasks.jsonl'
$holdoutPath = Join-Path $outFull 'holdout.tasks.jsonl'
[System.IO.File]::WriteAllLines($devPath, $devLines.ToArray(), [System.Text.UTF8Encoding]::new($false))
[System.IO.File]::WriteAllLines($holdoutPath, $holdoutLines.ToArray(), [System.Text.UTF8Encoding]::new($false))

$manifest = [ordered]@{
    schema_version = 'stablepay.agent-shopping-grouped-split.v1'
    source_tasks = [System.IO.Path]::GetRelativePath((Get-Location).Path, $tasksFull).Replace('\', '/')
    source_tasks_sha256 = Get-FileSha256 $tasksFull
    split_seed = $SplitSeed
    holdout_percent = $HoldoutPercent
    grouping = 'PLAN/NO_PLAN by category+exact item+format; CLARIFY by category+missing-field set'
    total_task_count = $records.Count
    unique_raw_prompt_count = $promptHashes.Count
    semantic_group_count = $groupBuckets.Count
    development = [ordered]@{ task_count = $devLines.Count; groups = $devGroups.Count; decisions = $devCounts; sha256 = Get-FileSha256 $devPath }
    holdout = [ordered]@{ task_count = $holdoutLines.Count; groups = $holdoutGroups.Count; decisions = $holdoutCounts; sha256 = Get-FileSha256 $holdoutPath }
    group_overlap_count = 0
    decision_counts_total = $decisionCounts
    labels_changed = $false
    model_outputs_seen_before_split = $false
    review_status = 'AI_GENERATED_SYNTHETIC_TASKS; PROGRAMMATIC_LABEL_VALIDATION; NOT_HUMAN_DOUBLE_REVIEWED'
}
$manifestText = $manifest | ConvertTo-Json -Depth 8
Write-Utf8NoBom (Join-Path $outFull 'split-manifest.json') $manifestText

[pscustomobject]@{
    output_dir = $outFull
    development_tasks = $devLines.Count
    holdout_tasks = $holdoutLines.Count
    development_decisions = $devCounts
    holdout_decisions = $holdoutCounts
    semantic_groups = $groupBuckets.Count
    prompt_uniqueness = $promptHashes.Count
    source_sha256 = $manifest.source_tasks_sha256
    development_sha256 = $manifest.development.sha256
    holdout_sha256 = $manifest.holdout.sha256
} | ConvertTo-Json -Depth 8
