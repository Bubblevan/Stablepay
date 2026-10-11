param(
    [Parameter(Mandatory = $true)] [string] $DraftDir,
    [Parameter(Mandatory = $true)] [string] $SourcePlanDraftDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Read-JsonLines([string] $Path) {
    $items = New-Object 'System.Collections.Generic.List[object]'
    foreach ($line in [System.IO.File]::ReadLines((Resolve-Path -LiteralPath $Path).Path)) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        try {
            $items.Add(($line | ConvertFrom-Json))
        } catch {
            throw "Invalid JSONL in '$Path': $($_.Exception.Message)"
        }
    }
    return $items.ToArray()
}

function Get-Sha256([string] $Path) {
    return 'sha256:' + (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Assert-Equal([object] $Actual, [object] $Expected, [string] $Name) {
    if ([string]$Actual -ne [string]$Expected) {
        throw "$Name mismatch: expected '$Expected', got '$Actual'."
    }
}

function Get-NormalizedPromptShape([object] $Task) {
    $text = [string]$Task.prompt
    $intent = $Task.gold_intent
    $replacements = New-Object 'System.Collections.Generic.List[object]'
    foreach ($field in @('item', 'required_format', 'preferred_style')) {
        $property = $intent.PSObject.Properties[$field]
        if ($null -eq $property) { continue }
        $value = [string]$property.Value
        if (-not [string]::IsNullOrWhiteSpace($value)) {
            $replacements.Add([pscustomobject]@{ Value = $value; Token = "<$($field.ToUpperInvariant())>" })
        }
    }
    $attributeProperty = $intent.PSObject.Properties['required_attributes']
    if ($null -ne $attributeProperty -and $null -ne $attributeProperty.Value) {
        foreach ($property in $attributeProperty.Value.PSObject.Properties) {
            $value = [string]$property.Value
            if (-not [string]::IsNullOrWhiteSpace($value)) {
                $replacements.Add([pscustomobject]@{ Value = $value; Token = '<ATTRIBUTE>' })
            }
        }
    }
    foreach ($replacement in ($replacements | Sort-Object { $_.Value.Length } -Descending)) {
        $text = $text.Replace([string]$replacement.Value, [string]$replacement.Token)
    }

    $categoryNames = @{
        audio = '音频内容'
        books = '书籍'
        'cloud-tools' = '云端工具'
        coffee = '咖啡'
        'design-assets' = '设计素材'
        'developer-tools' = '开发工具'
        education = '课程'
        'game-collectibles' = '数字收藏品'
        'stock-media' = '媒体素材'
        templates = '模板'
        'travel-guides' = '旅行资料'
        'wellness-guides' = '健康指南'
    }
    $categoryProperty = $intent.PSObject.Properties['category']
    if ($null -ne $categoryProperty) {
        $category = [string]$categoryProperty.Value
        if ($categoryNames.ContainsKey($category)) {
            $text = $text.Replace([string]$categoryNames[$category], '<CATEGORY>')
        }
    }

    $regionNames = @{
        'CN-EAST' = '华东'
        'CN-NORTH' = '华北'
        'CN-SOUTH' = '华南'
        'CN-WEST' = '西部'
    }
    $regionProperty = $intent.PSObject.Properties['region']
    $region = if ($null -ne $regionProperty) { [string]$regionProperty.Value } else { '' }
    if ($regionNames.ContainsKey($region)) {
        $text = $text.Replace([string]$regionNames[$region], '<REGION>')
    }
    $text = [regex]::Replace($text, '(?<!\d)\d+(?:\.\d{1,2})?(?=\s*(?:USDC|USD|美元|元))', '<AMOUNT>')
    return [regex]::Replace($text, '\s+', ' ').Trim()
}

foreach ($directory in @($DraftDir, $SourcePlanDraftDir)) {
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        throw "Required draft directory does not exist: $directory"
    }
}

$draftRoot = (Resolve-Path -LiteralPath $DraftDir).Path
$sourceRoot = (Resolve-Path -LiteralPath $SourcePlanDraftDir).Path
$draftTasksPath = Join-Path $draftRoot 'tasks.jsonl'
$draftManifestPath = Join-Path $draftRoot 'manifest.json'
$sourceTasksPath = Join-Path $sourceRoot 'tasks.jsonl'
$sourceManifestPath = Join-Path $sourceRoot 'manifest.json'
foreach ($path in @($draftTasksPath, $draftManifestPath, $sourceTasksPath, $sourceManifestPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required audit input does not exist: $path"
    }
}

$manifest = Get-Content -LiteralPath $draftManifestPath -Raw | ConvertFrom-Json
$sourceManifest = Get-Content -LiteralPath $sourceManifestPath -Raw | ConvertFrom-Json
$tasks = @(Read-JsonLines $draftTasksPath)
$sourceTasks = @(Read-JsonLines $sourceTasksPath)
$draftTaskHash = Get-Sha256 $draftTasksPath
$sourceTaskHash = Get-Sha256 $sourceTasksPath
$draftManifestHash = Get-Sha256 $draftManifestPath
$sourceManifestHash = Get-Sha256 $sourceManifestPath

Assert-Equal $manifest.schema_version 'stablepay.agent-shopping-challenge-draft.v1' 'Draft schema'
Assert-Equal $manifest.status 'AUTO_GENERATED_UNREVIEWED_DRAFT' 'Draft review status'
Assert-Equal $manifest.task_provenance 'GENERATED_FROM_LOCAL_SYNTHETIC_MERCHANT_SNAPSHOT' 'Draft provenance'
Assert-Equal $manifest.hashes.tasks_sha256 $draftTaskHash 'Draft task-file hash'
Assert-Equal $sourceManifest.hashes.tasks_sha256 $sourceTaskHash 'Source task-file hash'
foreach ($hashName in @('catalog_sha256', 'inventory_sha256', 'source_manifest_sha256')) {
    Assert-Equal $manifest.hashes.$hashName $sourceManifest.hashes.$hashName "Source snapshot $hashName"
}
if ($tasks.Count -ne [int]$manifest.task_count -or $tasks.Count -ne [int]$sourceManifest.task_count) {
    throw 'Draft/source task count differs from its manifest.'
}
if ([string]$manifest.public_dataset_use -notlike 'NONE*') {
    throw 'This local diagnostic audit expects public benchmark datasets to remain reference-only.'
}

$sourceByID = @{}
foreach ($task in $sourceTasks) {
    $id = [string]$task.task_id
    if ([string]::IsNullOrWhiteSpace($id) -or $sourceByID.ContainsKey($id)) {
        throw "Source task IDs must be non-empty and unique: '$id'"
    }
    $sourceByID[$id] = $task
}

$taskByID = @{}
foreach ($task in $tasks) {
    $id = [string]$task.task_id
    if ([string]::IsNullOrWhiteSpace($id) -or $taskByID.ContainsKey($id) -or -not $sourceByID.ContainsKey($id)) {
        throw "Draft task IDs must be unique and match the source plan draft: '$id'"
    }
    if ([string]::IsNullOrWhiteSpace([string]$task.prompt)) {
        throw "Draft task '$id' has an empty prompt."
    }
    $taskByID[$id] = $task
}
if ($taskByID.Count -ne $sourceByID.Count) {
    throw 'Draft and source task ID sets do not match.'
}

$decisionCounts = [ordered]@{}
foreach ($group in ($tasks | Group-Object { [string]$_.expected_disposition } | Sort-Object Name)) {
    $decisionCounts[[string]$group.Name] = $group.Count
}
$expectedClarifyCount = [int][math]::Floor($tasks.Count * 0.15)
$expectedNoPlanCount = $expectedClarifyCount
$expectedPlanCount = $tasks.Count - $expectedClarifyCount - $expectedNoPlanCount
Assert-Equal $decisionCounts['PLAN'] $expectedPlanCount 'PLAN task count'
Assert-Equal $decisionCounts['NO_PLAN'] $expectedNoPlanCount 'NO_PLAN task count'
Assert-Equal $decisionCounts['CLARIFY'] $expectedClarifyCount 'CLARIFY task count'

$clarifyTasks = @($tasks | Where-Object { $_.expected_disposition -eq 'CLARIFY' })
$planTasks = @($tasks | Where-Object { $_.expected_disposition -eq 'PLAN' })
$clarificationPatterns = @{}
$literalLeaks = 0
$missingKnownValues = 0
$supportedFields = @('item', 'required_format', 'preferred_style')
foreach ($task in $clarifyTasks) {
    $fields = @($task.required_missing_fields | ForEach-Object { [string]$_ })
    if ($fields.Count -lt 1 -or $fields.Count -gt 2) {
        throw "Clarification task '$($task.task_id)' must omit one or two fields."
    }
    if (@($fields | Select-Object -Unique).Count -ne $fields.Count) {
        throw "Clarification task '$($task.task_id)' repeats a missing-field label."
    }
    foreach ($field in $fields) {
        if ($field -notin $supportedFields) { throw "Unsupported missing-field label '$field' in '$($task.task_id)'." }
    }
    $orderedFields = @($supportedFields | Where-Object { $_ -in $fields })
    $pattern = $orderedFields -join '+'
    $expectedScenario = 'CLARIFY+MISSING_' + (($orderedFields -join '_').ToUpperInvariant())
    Assert-Equal $task.scenario $expectedScenario "Clarification scenario for $($task.task_id)"
    $previousCount = 0
    if ($clarificationPatterns.ContainsKey($pattern)) { $previousCount = [int]$clarificationPatterns[$pattern] }
    $clarificationPatterns[$pattern] = $previousCount + 1

    $source = $sourceByID[[string]$task.task_id]
    foreach ($field in $supportedFields) {
        $isMissing = $field -in $orderedFields
        $currentValue = [string]$task.gold_intent.$field
        $sourceValue = [string]$source.gold_intent.$field
        if ($isMissing -eq ($currentValue -ne '')) {
            throw "Clarification task '$($task.task_id)' disagrees on whether '$field' is missing."
        }
        if ($isMissing) {
            if ($sourceValue -and $task.prompt.Contains($sourceValue)) { $literalLeaks++ }
        } elseif ($currentValue -and -not $task.prompt.Contains($currentValue)) {
            $missingKnownValues++
        }
    }
}

if ($expectedClarifyCount -ge 6) {
    $decisionCriticalPatterns = @('item', 'required_format', 'item+required_format', 'item+preferred_style', 'required_format+preferred_style')
    $legacyPatterns = @($decisionCriticalPatterns) + 'preferred_style'
    $hasLegacyStyleOnly = $clarificationPatterns.ContainsKey('preferred_style')
    $patternsToCheck = if ($hasLegacyStyleOnly) { $legacyPatterns } else { $decisionCriticalPatterns }
    $minimumPerPattern = [int][math]::Floor($expectedClarifyCount / $patternsToCheck.Count)
    $maximumPerPattern = [int][math]::Ceiling($expectedClarifyCount / $patternsToCheck.Count)
    if ($clarificationPatterns.Count -ne $patternsToCheck.Count) {
        throw "Unexpected clarification pattern count $($clarificationPatterns.Count); found patterns: $($clarificationPatterns.Keys -join ', ')."
    }
    foreach ($pattern in $patternsToCheck) {
        $count = [int]$clarificationPatterns[$pattern]
        if ($count -lt $minimumPerPattern -or $count -gt $maximumPerPattern) {
            throw "Clarification pattern '$pattern' is imbalanced: $count, expected $minimumPerPattern-$maximumPerPattern."
        }
    }
}

$promptGroups = @($tasks | Group-Object { [string]$_.prompt })
$duplicatePromptGroups = @($promptGroups | Where-Object { $_.Count -gt 1 })
$noPlanAnswerCues = @($tasks | Where-Object {
    $_.expected_disposition -eq 'NO_PLAN' -and $_.prompt -match '无方案|没有可行商品|不要提高预算|不要下单|若没有商品'
}).Count
$clarifyAnswerCues = @($tasks | Where-Object {
    $_.expected_disposition -eq 'CLARIFY' -and $_.prompt -match '只追问|仍缺少|必要信息|不要自行猜测|不要下单'
}).Count
$optionalStyleOnlyClarifications = @($clarifyTasks | Where-Object {
    @($_.required_missing_fields | ForEach-Object { [string]$_ }).Count -eq 1 -and
    [string]$_.required_missing_fields[0] -eq 'preferred_style'
}).Count
$styleUnspecifiedPlans = @($planTasks | Where-Object { [string]$_.scenario -match '\+STYLE_UNSPECIFIED' })
$styleOmissionIntentLeaks = 0
$styleOmissionPromptLeaks = 0
foreach ($task in $styleUnspecifiedPlans) {
    $source = $sourceByID[[string]$task.task_id]
    $sourceStyle = [string]$source.gold_intent.preferred_style
    if ([string]$task.gold_intent.preferred_style -ne '') { $styleOmissionIntentLeaks++ }
    if ($sourceStyle -and $task.prompt.Contains($sourceStyle)) { $styleOmissionPromptLeaks++ }
    foreach ($attribute in $task.gold_intent.required_attributes.PSObject.Properties) {
        if ([string]$attribute.Value -eq $sourceStyle) { $styleOmissionPromptLeaks++ }
    }
}
$planAnswerCues = @($tasks | Where-Object {
    $_.expected_disposition -eq 'PLAN' -and $_.prompt -match '可以购买|已有可行商品|符合条件的商品存在'
}).Count
$scenarioCounts = [ordered]@{}
foreach ($group in ($tasks | Group-Object { [string]$_.scenario } | Sort-Object Name)) {
    $scenarioCounts[[string]$group.Name] = $group.Count
}
$promptShapes = @(
    foreach ($task in $tasks) {
        [pscustomobject]@{
            Disposition = [string]$task.expected_disposition
            Shape = Get-NormalizedPromptShape $task
        }
    }
)
$normalizedShapeCounts = @(
    foreach ($group in ($promptShapes | Group-Object Disposition | Sort-Object Name)) {
        $shapeGroups = @($group.Group | Group-Object Shape)
        [pscustomobject]@{
            disposition = [string]$group.Name
            task_count = $group.Count
            normalized_surface_shape_count = $shapeGroups.Count
            largest_exact_shape_cluster = [int](($shapeGroups | Measure-Object -Property Count -Maximum).Maximum)
            largest_cluster_sizes = @($shapeGroups | Sort-Object Count -Descending | Select-Object -First 5 -ExpandProperty Count)
        }
    }
)
$normalizedShapeCount = @($promptShapes | Select-Object -ExpandProperty Shape -Unique).Count
$auditStatus = if ($duplicatePromptGroups.Count -eq 0 -and $literalLeaks -eq 0 -and $missingKnownValues -eq 0 -and
    $noPlanAnswerCues -eq 0 -and $clarifyAnswerCues -eq 0 -and $planAnswerCues -eq 0 -and
    $optionalStyleOnlyClarifications -eq 0 -and
    $styleUnspecifiedPlans.Count -eq [int][math]::Floor($planTasks.Count / 5) -and
    $styleOmissionIntentLeaks -eq 0 -and $styleOmissionPromptLeaks -eq 0) {
    'PASS_STRUCTURE_ONLY_NOT_HUMAN_REVIEWED'
} elseif ($noPlanAnswerCues -gt 0 -or $clarifyAnswerCues -gt 0 -or $planAnswerCues -gt 0) {
    'FAIL_DIRECT_DISPOSITION_CUES'
} elseif ($optionalStyleOnlyClarifications -gt 0) {
    'FAIL_OPTIONAL_STYLE_BLOCKS_PLAN'
} elseif ($styleUnspecifiedPlans.Count -ne [int][math]::Floor($planTasks.Count / 5) -or
    $styleOmissionIntentLeaks -gt 0 -or $styleOmissionPromptLeaks -gt 0) {
    'FAIL_OPTIONAL_STYLE_PLAN_COVERAGE'
} else {
    throw 'Draft structural checks found duplicated prompt strings or clarification prompt/value mismatches.'
}

$report = [ordered]@{
    audit = 'READ_ONLY_AGENT_SHOPPING_DRAFT_STRUCTURE'
    status = $auditStatus
    review_status = [string]$manifest.status
    resume_evidence_eligible = $false
    task_count = $tasks.Count
    unique_prompt_count = $promptGroups.Count
    normalized_prompt_shape_audit = [ordered]@{
        method = 'replace exact gold intent item/format/style and hard-attribute values, mapped Chinese category/region names, and currency-adjacent amounts with placeholders; then count exact remaining text shapes by disposition'
        normalized_surface_shape_count = $normalizedShapeCount
        by_disposition = $normalizedShapeCounts
        caveat = 'Heuristic surface-template diagnostic only; aliases or synonyms not present in gold fields may remain distinct, and equal text shapes do not establish semantic equivalence or task difficulty.'
    }
    decision_counts = $decisionCounts
    scenario_counts = $scenarioCounts
    clarification_missing_patterns = $clarificationPatterns
    clarification_known_values_missing_from_prompt = $missingKnownValues
    clarification_omitted_source_values_literal_leaked = $literalLeaks
    direct_disposition_answer_cues = [ordered]@{
        plan = $planAnswerCues
        no_plan = $noPlanAnswerCues
        clarify = $clarifyAnswerCues
    }
    optional_style_only_clarification_count = $optionalStyleOnlyClarifications
    optional_style_plan_control = [ordered]@{
        observed_plan_tasks = $styleUnspecifiedPlans.Count
        expected_plan_tasks = [int][math]::Floor($planTasks.Count / 5)
        intent_leaks = $styleOmissionIntentLeaks
        prompt_or_attribute_leaks = $styleOmissionPromptLeaks
    }
    task_sha256 = $draftTaskHash
    manifest_sha256 = $draftManifestHash
    source_task_sha256 = $sourceTaskHash
    source_manifest_sha256 = $sourceManifestHash
    task_bytes = (Get-Item -LiteralPath $draftTasksPath).Length
    caveat = 'Structural and fixed-keyword checks only. Zero direct-cue matches do not prove semantic independence or naturalness; this does not establish human authorship, semantic hardness, independent relevance, or model quality.'
}
$report | ConvertTo-Json -Depth 8
if ($auditStatus -eq 'FAIL_DIRECT_DISPOSITION_CUES') {
    throw "Draft contains direct disposition answer cues: PLAN=$planAnswerCues NO_PLAN=$noPlanAnswerCues CLARIFY=$clarifyAnswerCues"
}
if ($auditStatus -eq 'FAIL_OPTIONAL_STYLE_BLOCKS_PLAN') {
    throw "Draft marks $optionalStyleOnlyClarifications style-only omissions as blocking clarification tasks"
}
if ($auditStatus -eq 'FAIL_OPTIONAL_STYLE_PLAN_COVERAGE') {
    throw "Draft optional-style plan control mismatch: observed=$($styleUnspecifiedPlans.Count) expected=$([int][math]::Floor($planTasks.Count / 5)) intent_leaks=$styleOmissionIntentLeaks prompt_or_attribute_leaks=$styleOmissionPromptLeaks"
}
