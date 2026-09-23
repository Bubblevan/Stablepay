[CmdletBinding()]
param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [string]$OutputRoot = '',
  [string]$CrashWindows = 'DISCOVERING,INVOKING,NEGOTIATING,PAYING,CLAIMING,INVOKING_DELIVERY,VALIDATING_DELIVERY,RECOVERING',
  [string]$SmokeWindow = 'PAYING',
  [int]$TrialsPerWindow = 20,
  [int]$PollMilliseconds = 5
)

$ErrorActionPreference = 'Stop'
$runtimeRoot = Join-Path $RepoRoot 'commerce-runtime'
$dotenv = Join-Path $RepoRoot '.env'
$e4Script = Join-Path $RepoRoot 'scripts\benchmark-e4-chaos.ps1'
$bodyPath = Join-Path $RepoRoot 'testdata\benchmark\e4-submit-body.json'

function Import-BenchmarkDotEnv([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { throw "Missing dotenv file: $Path" }
  $allowed = @('COMMERCE_RUNTIME_MYSQL_DSN', 'COMMERCE_RUNTIME_API_TOKEN')
  foreach ($line in Get-Content -LiteralPath $Path) {
    if ($line -match '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') {
      $name = $matches[1]
      if ($name -notin $allowed) { continue }
      $value = $matches[2].Trim().Trim('"').Trim("'")
      if (-not [string]::IsNullOrWhiteSpace($value)) {
        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
      }
    }
  }
}

function Invoke-Go([string[]]$Arguments) {
  Push-Location $runtimeRoot
  try {
    & go @Arguments
    if ($LASTEXITCODE -ne 0) { throw "go $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
  } finally { Pop-Location }
}

$env:GOCACHE = Join-Path $RepoRoot '.local-run\gocache-e4'
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null

function Set-JsonProperty($Object, [string]$Name, $Value) {
  $Object | Add-Member -MemberType NoteProperty -Name $Name -Value $Value -Force
}

function Invoke-RunAndAudit([string]$Name, [string]$WindowList, [int]$Trials, [string]$Executable, [string]$Schema, [string]$NewDsn, [string]$RunOutput) {
  New-Item -ItemType Directory -Force -Path $RunOutput | Out-Null
  $env:OUTPUT_ROOT = $RunOutput
  $env:E4_MYSQL_SCHEMA = $Schema
  $env:COMMERCE_RUNTIME_MYSQL_DSN = $NewDsn
  $env:COMMERCE_RUNTIME_HTTP_ADDR = '127.0.0.1:18091'
  $env:COMMERCE_RUNTIME_MYSQL_MAX_OPEN_CONNS = '20'
  $env:COMMERCE_RUNTIME_MYSQL_MAX_IDLE_CONNS = '10'
  $env:E4_MOCK_DELAY_MS = '150'
  $env:API_TOKEN = $env:COMMERCE_RUNTIME_API_TOKEN

  & powershell -NoProfile -ExecutionPolicy Bypass -File $e4Script `
    -RuntimeExecutable $Executable `
    -RuntimeWorkingDirectory $runtimeRoot `
    -BaseUrl 'http://127.0.0.1:18091' `
    -SubmitBodyPath $bodyPath `
    -DotEnvPath (Join-Path $RunOutput '.no-dotenv') `
    -CrashWindows $WindowList `
    -TrialsPerWindow $Trials `
    -PollMilliseconds $PollMilliseconds `
    -TerminalTimeoutSeconds 30 `
    -OutputRoot $RunOutput
  if ($LASTEXITCODE -ne 0) { throw "$Name E4 runner failed with exit code $LASTEXITCODE" }

  $auditPath = Join-Path $RunOutput 'audit.jsonl'
  Invoke-Go @('run', './cmd/e4-mysql-audit', 'audit', '-trials', (Join-Path $RunOutput 'trials.jsonl'), '-out', $auditPath)
  $summaryPath = Join-Path $RunOutput 'audit-summary.json'
  $summary = Get-Content -Raw -LiteralPath $summaryPath | ConvertFrom-Json
  if ($summary.status -ne 'PASS') { throw "$Name MySQL audit did not pass; see $summaryPath" }

  $manifestPath = Join-Path $RunOutput 'manifest.json'
  $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
  $manifest.status = 'COMPLETE'
  Set-JsonProperty $manifest 'FROZEN_RUNTIME_CHANGED' 'NO'
  Set-JsonProperty $manifest 'audit_status' 'PASS'
  Set-JsonProperty $manifest 'audit_file' 'audit.jsonl'
  Set-JsonProperty $manifest 'mysql_schema' $Schema
  $manifest | ConvertTo-Json -Depth 20 | Set-Content -Encoding UTF8 -LiteralPath $manifestPath

  $metricsPath = Join-Path $RunOutput 'metrics.json'
  $metrics = Get-Content -Raw -LiteralPath $metricsPath | ConvertFrom-Json
  $metrics.status = 'COMPLETE'
  Set-JsonProperty $metrics 'FROZEN_RUNTIME_CHANGED' 'NO'
  Set-JsonProperty $metrics 'correctness_status' 'PASS'
  Set-JsonProperty $metrics 'resume_success' ([ordered]@{ numerator = $summary.passed; denominator = $summary.trials })
  Set-JsonProperty $metrics 'duplicate_payment_intent' $summary.duplicate_payment_intent_rows
  Set-JsonProperty $metrics 'duplicate_settlement' $summary.duplicate_payment_settled_rows
  Set-JsonProperty $metrics 'duplicate_payment_intent_rows' $summary.duplicate_payment_intent_rows
  Set-JsonProperty $metrics 'duplicate_PAYMENT_SETTLED_rows' $summary.duplicate_payment_settled_rows
  Set-JsonProperty $metrics 'payment_intent_count_mismatch' $summary.payment_intent_count_mismatch
  Set-JsonProperty $metrics 'payment_settled_count_mismatch' $summary.payment_settled_count_mismatch
  Set-JsonProperty $metrics 'budget_drift' $summary.budget_drift
  Set-JsonProperty $metrics 'orphaned_episodes' $summary.orphaned_episodes
  Set-JsonProperty $metrics 'stuck_episodes' $summary.stuck_episodes
  Set-JsonProperty $metrics 'audit' 'audit-summary.json'
  $metrics | ConvertTo-Json -Depth 20 | Set-Content -Encoding UTF8 -LiteralPath $metricsPath
  @('', '## MySQL correctness audit', '', 'Status: **PASS**', '', "Trials audited by episode_id: $($summary.passed)/$($summary.trials)", "Duplicate PaymentIntent query rows: $($summary.duplicate_payment_intent_rows)", "Duplicate PAYMENT_SETTLED query rows: $($summary.duplicate_payment_settled_rows)", "PaymentIntent count mismatches: $($summary.payment_intent_count_mismatch)", "PAYMENT_SETTLED count mismatches: $($summary.payment_settled_count_mismatch)", "Budget projection drift: $($summary.budget_drift)", "Orphaned episodes: $($summary.orphaned_episodes)", "Stuck/non-terminal episodes: $($summary.stuck_episodes)", '', 'Budget audit recomputed expected_consumed, expected_available, and expected_sunk_cost from ledger facts and matched each persisted episode projection.') | Add-Content -Encoding UTF8 -LiteralPath (Join-Path $RunOutput 'report.md')
  Write-Host "[$Name] MySQL audit passed: $($summary.passed)/$($summary.trials); duplicates PI=$($summary.duplicate_payment_intent_rows), settlements=$($summary.duplicate_payment_settled_rows); budget drift=$($summary.budget_drift); orphan=$($summary.orphaned_episodes); stuck=$($summary.stuck_episodes)"
}

Import-BenchmarkDotEnv $dotenv
if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_MYSQL_DSN) -or [string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_API_TOKEN)) {
  throw '.env must define COMMERCE_RUNTIME_MYSQL_DSN and COMMERCE_RUNTIME_API_TOKEN'
}
if (-not (Test-Path -LiteralPath $bodyPath)) { throw "Missing E4 submit body: $bodyPath" }

if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
  $stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
  $OutputRoot = Join-Path $RepoRoot ".local-run\resume-benchmark\e4-local-$stamp-$((Get-Random -Minimum 1000 -Maximum 9999))"
}
$OutputRoot = [System.IO.Path]::GetFullPath($OutputRoot)
New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null

$portInUse = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -eq 18091 }
if ($portInUse) { throw 'E4 isolated HTTP port 18091 is already in use; refusing to attach to an unrelated Runtime' }

$baseDsn = $env:COMMERCE_RUNTIME_MYSQL_DSN
$dsnMatch = [regex]::Match($baseDsn, '^(?<prefix>.*@tcp\([^)]*\)/)(?<database>[^?]*)(?<query>\?.*)?$')
if (-not $dsnMatch.Success) { throw 'MySQL DSN must use go-sql-driver/mysql tcp(host:port)/database form' }
$schemaSuffix = Get-Random -Minimum 1000 -Maximum 9999
$schema = 'stablepay_e4_' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss') + '_' + $schemaSuffix
$newDsn = $dsnMatch.Groups['prefix'].Value + $schema + $dsnMatch.Groups['query'].Value
$env:E4_MYSQL_SCHEMA = $schema
Invoke-Go @('run', './cmd/e4-mysql-audit', 'create-schema', '-name', $schema)

$runtimeExe = Join-Path $OutputRoot 'commerce-runtime-e4-local.exe'
Invoke-Go @('build', '-trimpath', '-o', $runtimeExe, './cmd/e4-local-runtime')

$setup = [ordered]@{
  benchmark = 'E4'
  mode = 'benchmark-only deterministic local adapters'
  mysql_schema = $schema
  mysql_host = $dsnMatch.Groups['prefix'].Value -replace '^.*@tcp\(', '' -replace '\)/$', ''
  settlement_network = 'e4-local'
  payment_adapter = 'MySQL-backed idempotent mock; no chain client'
  runtime_executable = 'commerce-runtime-e4-local.exe'
  runtime_http_addr = '127.0.0.1:18091'
  body_budget_limit_minor = 1000
  frozen_runtime_changed = 'NO'
}
$setup | ConvertTo-Json -Depth 5 | Set-Content -Encoding UTF8 -LiteralPath (Join-Path $OutputRoot 'setup.json')

Invoke-RunAndAudit 'smoke' $SmokeWindow 1 $runtimeExe $schema $newDsn (Join-Path $OutputRoot 'smoke')
Invoke-RunAndAudit 'full-matrix' $CrashWindows $TrialsPerWindow $runtimeExe $schema $newDsn (Join-Path $OutputRoot 'full')

Write-Host "E4 local matrix complete. Output: $OutputRoot"
Write-Host "Isolated MySQL schema retained for audit: $schema"
