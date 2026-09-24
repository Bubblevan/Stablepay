[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$AuditJsonlPath,
    [string]$OutputPath = ''
)

$ErrorActionPreference = 'Stop'
$AuditJsonlPath = (Resolve-Path -LiteralPath $AuditJsonlPath).Path
$rows = @(Get-Content -Encoding UTF8 -LiteralPath $AuditJsonlPath | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_ | ConvertFrom-Json })
if ($rows.Count -eq 0) { throw 'Audit input contains no per-episode rows' }

$sum = {
    param($Selector)
    $total = 0
    foreach ($row in $rows) { $total += [int](& $Selector $row) }
    return $total
}
$duplicateIntentRows = & $sum { param($row) @($row.duplicate_payment_intent_rows).Count }
$duplicateSettlementRows = & $sum { param($row) @($row.duplicate_payment_settled_rows).Count }
$intentCountMismatch = & $sum { param($row) if ([int]$row.payment_intent_count -ne 1) { 1 } else { 0 } }
$settlementCountMismatch = & $sum { param($row) if ([int]$row.payment_settled_count -ne 1) { 1 } else { 0 } }
$budgetDrift = & $sum { param($row) if (-not $row.budget.consistent) { 1 } else { 0 } }
$orphaned = & $sum { param($row) if ($row.state.orphan) { 1 } else { 0 } }
$stuck = & $sum { param($row) if ($row.state.stuck) { 1 } else { 0 } }
$notFulfilled = & $sum { param($row) if ($row.state.episode_state -ne 'FULFILLED') { 1 } else { 0 } }
$notCompleted = & $sum { param($row) if ($row.state.execution_state -ne 'COMPLETED') { 1 } else { 0 } }
$expectedConsumed = (& $sum { param($row) $row.budget.expected_consumed })
$actualConsumed = (& $sum { param($row) $row.budget.actual_consumed })
$expectedAvailable = (& $sum { param($row) $row.budget.expected_available })
$actualAvailable = (& $sum { param($row) $row.budget.actual_available })
$expectedSunkCost = (& $sum { param($row) $row.budget.expected_sunk_cost })
$actualSunkCost = (& $sum { param($row) $row.budget.actual_sunk_cost })
$issues = $duplicateIntentRows + $duplicateSettlementRows + $intentCountMismatch + $settlementCountMismatch + $budgetDrift + $orphaned + $stuck + $notFulfilled + $notCompleted
$summary = [ordered]@{
    benchmark = 'Minikube steady-state Agent payment state-machine audit'
    business_invariants_status = if ($issues -eq 0) { 'PASS' } else { 'FAIL' }
    crash_recovery_status = 'NOT_TESTED'
    crash_recovery_note = 'No Runtime restart was injected in this steady-state smoke/load run; this is not an E4 crash-recovery claim.'
    adapter_mode = 'benchmark-only deterministic local Merchant/Payment/Verification mocks; no chain client'
    audited_episodes = $rows.Count
    payment_intent_rows_per_episode = 'exactly 1'
    duplicate_payment_intent_query_rows = $duplicateIntentRows
    payment_intent_count_mismatch = $intentCountMismatch
    payment_settled_rows_per_episode = 'exactly 1'
    duplicate_PAYMENT_SETTLED_query_rows = $duplicateSettlementRows
    payment_settled_count_mismatch = $settlementCountMismatch
    budget_projection_drift = $budgetDrift
    budget_totals_minor = @{
        expected_consumed = $expectedConsumed
        actual_consumed = $actualConsumed
        expected_available = $expectedAvailable
        actual_available = $actualAvailable
        expected_sunk_cost = $expectedSunkCost
        actual_sunk_cost = $actualSunkCost
    }
    orphaned_episodes = $orphaned
    stuck_episodes = $stuck
    episodes_not_fulfilled = $notFulfilled
    executions_not_completed = $notCompleted
}
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path (Split-Path -Parent $AuditJsonlPath) 'minikube-audit-summary.json'
}
$summary | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 -LiteralPath $OutputPath
$summary | ConvertTo-Json -Depth 10
if ($summary.business_invariants_status -ne 'PASS') { exit 1 }
