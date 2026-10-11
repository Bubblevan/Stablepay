param(
  [Parameter(Mandatory = $true)]
  [string]$RunDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$expectedContext = 'stablepay-six'
$namespaceName = 'stablepay-e3-local'
$context = (kubectl config current-context).Trim()
if ($LASTEXITCODE -ne 0 -or $context -ne $expectedContext) {
  throw "Refusing E3 audit outside Kubernetes context '$expectedContext' (observed '$context')."
}

$namespace = kubectl get namespace $namespaceName -o json | ConvertFrom-Json
if ($LASTEXITCODE -ne 0) { throw "Could not inspect namespace '$namespaceName'." }
$boundary = $namespace.metadata.labels.PSObject.Properties['stablepay.io/payment-boundary']
if (-not $boundary -or $boundary.Value -ne 'deterministic-local-only') {
  throw "Refusing audit: namespace '$namespaceName' is not labeled deterministic-local-only."
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$localRunRoot = (Resolve-Path (Join-Path $repoRoot '.local-run')).Path.TrimEnd('\') + '\'
$resolvedRunDirectory = (Resolve-Path -LiteralPath $RunDirectory).Path
if (-not $resolvedRunDirectory.StartsWith($localRunRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
  throw "RunDirectory must be below '$localRunRoot'."
}

$manifestPath = Join-Path $resolvedRunDirectory 'manifest.json'
$metricsPath = Join-Path $resolvedRunDirectory 'metrics.json'
$outputPath = Join-Path $resolvedRunDirectory 'audit-summary.json'
foreach ($path in @($manifestPath, $metricsPath)) {
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing run artifact: $path" }
}
if (Test-Path -LiteralPath $outputPath) {
  throw "Refusing to overwrite existing audit evidence: $outputPath"
}

$mysqlDeployment = kubectl get deployment stablepay-e3-mysql -n $namespaceName -o json | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or [int]$mysqlDeployment.status.readyReplicas -ne 1) {
  throw 'The isolated E3 MySQL deployment is not exactly one ready replica.'
}

$sql = @'
SELECT JSON_OBJECT(
  'episode_count', (SELECT COUNT(*) FROM commerce_episodes),
  'episode_state_counts', (
    SELECT JSON_OBJECTAGG(state, state_count)
    FROM (SELECT state, COUNT(*) AS state_count FROM commerce_episodes GROUP BY state) AS states
  ),
  'execution_status_counts', (
    SELECT JSON_OBJECTAGG(status, status_count)
    FROM (SELECT status, COUNT(*) AS status_count FROM episode_execution_status GROUP BY status) AS executions
  ),
  'payment_intent_rows', (SELECT COUNT(*) FROM payment_intents),
  'payment_intent_episodes', (SELECT COUNT(DISTINCT episode_id) FROM payment_intents),
  'duplicate_payment_intent_episode_groups', (
    SELECT COUNT(*) FROM (
      SELECT episode_id FROM payment_intents GROUP BY episode_id HAVING COUNT(*) > 1
    ) AS duplicate_intents
  ),
  'payment_settlement_rows', (
    SELECT COUNT(*) FROM ledger_entries WHERE type = 'PAYMENT_SETTLED'
  ),
  'payment_settlement_episodes', (
    SELECT COUNT(DISTINCT episode_id) FROM ledger_entries WHERE type = 'PAYMENT_SETTLED'
  ),
  'duplicate_payment_settlement_groups', (
    SELECT COUNT(*) FROM (
      SELECT episode_id, payment_intent_id
      FROM ledger_entries WHERE type = 'PAYMENT_SETTLED'
      GROUP BY episode_id, payment_intent_id HAVING COUNT(*) > 1
    ) AS duplicate_settlements
  ),
  'nonterminal_or_incomplete_execution_episodes', (
    SELECT COUNT(*)
    FROM commerce_episodes AS e
    LEFT JOIN episode_execution_status AS x ON x.episode_id = e.episode_id
    WHERE e.state NOT IN ('FULFILLED', 'FAILED', 'BLOCKED', 'ABORTED', 'EXPIRED', 'DISPUTED')
       OR x.episode_id IS NULL OR x.status <> 'COMPLETED'
  ),
  'budget_projection_drift_episodes', (
    SELECT COUNT(*)
    FROM commerce_episodes AS e
    LEFT JOIN (
      SELECT episode_id,
             SUM(CASE WHEN type = 'BUDGET_RESERVED' THEN amount_minor ELSE 0 END) AS reserved,
             SUM(CASE WHEN type = 'BUDGET_RELEASED' THEN amount_minor ELSE 0 END) AS released,
             SUM(CASE WHEN type = 'PAYMENT_SETTLED' THEN amount_minor ELSE 0 END) AS settled,
             SUM(CASE WHEN type = 'REFUND_CONFIRMED' THEN amount_minor ELSE 0 END) AS refunded
      FROM ledger_entries GROUP BY episode_id
    ) AS l ON l.episode_id = e.episode_id
    WHERE e.consumed_amount <> IF(e.refund_reusable = 0, COALESCE(l.settled, 0),
                                  GREATEST(0, COALESCE(l.settled, 0) - COALESCE(l.refunded, 0)))
       OR e.available_budget <> e.budget_limit_minor
          - IF(e.refund_reusable = 0, COALESCE(l.settled, 0),
               GREATEST(0, COALESCE(l.settled, 0) - COALESCE(l.refunded, 0)))
          - (COALESCE(l.reserved, 0) - COALESCE(l.released, 0))
       OR e.sunk_cost <> GREATEST(0, COALESCE(l.settled, 0) - COALESCE(l.refunded, 0))
  )
);
'@
$sqlBase64 = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($sql))
$shellCommand = 'printf %s ' + $sqlBase64 + ' | base64 -d | MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --batch --raw --skip-column-names -uroot -D "$MYSQL_DATABASE"'
$databaseResult = @(kubectl exec -n $namespaceName deployment/stablepay-e3-mysql -- sh -c $shellCommand 2>$null)
if ($LASTEXITCODE -ne 0 -or $databaseResult.Count -ne 1) {
  throw 'The read-only SQL audit did not return exactly one result row.'
}
$databaseAudit = $databaseResult[0] | ConvertFrom-Json

$episodeCount = [long]$databaseAudit.episode_count
$checks = [ordered]@{
  episodes_present = $episodeCount -gt 0
  all_episodes_terminal_and_execution_complete = [long]$databaseAudit.nonterminal_or_incomplete_execution_episodes -eq 0
  one_payment_intent_per_episode = [long]$databaseAudit.payment_intent_rows -eq $episodeCount -and [long]$databaseAudit.payment_intent_episodes -eq $episodeCount
  no_duplicate_payment_intent_groups = [long]$databaseAudit.duplicate_payment_intent_episode_groups -eq 0
  one_payment_settlement_per_episode = [long]$databaseAudit.payment_settlement_rows -eq $episodeCount -and [long]$databaseAudit.payment_settlement_episodes -eq $episodeCount
  no_duplicate_payment_settlement_groups = [long]$databaseAudit.duplicate_payment_settlement_groups -eq 0
  budget_projection_consistent = [long]$databaseAudit.budget_projection_drift_episodes -eq 0
}
$passed = @($checks.Values | Where-Object { -not $_ }).Count -eq 0
$manifestHash = (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
$metricsHash = (Get-FileHash -LiteralPath $metricsPath -Algorithm SHA256).Hash.ToLowerInvariant()
$audit = [ordered]@{
  audit = 'E3_ISOLATED_MYSQL_READ_ONLY'
  status = $(if ($passed) { 'PASS' } else { 'FAIL' })
  run_id = Split-Path -Leaf $resolvedRunDirectory
  audited_at_utc = [DateTime]::UtcNow.ToString('o')
  kubernetes_context = $context
  namespace = $namespaceName
  payment_boundary = 'deterministic-local-only'
  source_sha256 = [ordered]@{ manifest = "sha256:$manifestHash"; metrics = "sha256:$metricsHash" }
  database = $databaseAudit
  assertions = $checks
  caveat = 'This audit covers only the isolated local E3 MySQL schema and synthetic deterministic payment adapter; it is not Devnet or production payment evidence.'
}
$audit | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $outputPath -Encoding UTF8
$audit | ConvertTo-Json -Depth 12
if (-not $passed) { exit 1 }
