param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [string]$RuntimeExecutable = $(if ($env:RUNTIME_EXECUTABLE) { $env:RUNTIME_EXECUTABLE } else { '' }),
  [string]$RuntimeArguments = $(if ($env:RUNTIME_ARGUMENTS) { $env:RUNTIME_ARGUMENTS } else { '' }),
  [string]$RuntimeWorkingDirectory = $(if ($env:RUNTIME_WORKDIR) { $env:RUNTIME_WORKDIR } else { (Join-Path $RepoRoot 'commerce-runtime') }),
  [string]$BaseUrl = $(if ($env:BASE_URL) { $env:BASE_URL } else { 'http://127.0.0.1:8090' }),
  [string]$SubmitPath = $(if ($env:SUBMIT_PATH) { $env:SUBMIT_PATH } else { '/v1/episodes' }),
  [string]$StatusPathTemplate = $(if ($env:STATUS_PATH_TEMPLATE) { $env:STATUS_PATH_TEMPLATE } else { '/v1/episodes/{0}' }),
  [string]$SubmitBodyPath = $(if ($env:SUBMIT_BODY_PATH) { $env:SUBMIT_BODY_PATH } else { '' }),
  [string]$ReadyPath = $(if ($env:READY_PATH) { $env:READY_PATH } else { '/readyz' }),
  [string]$CrashWindows = $(if ($env:CRASH_WINDOWS) { $env:CRASH_WINDOWS } else { 'DISCOVERING,INVOKING,NEGOTIATING,PAYING,CLAIMING,INVOKING_DELIVERY,VALIDATING_DELIVERY,RECOVERING' }),
  [int]$TrialsPerWindow = $(if ($env:TRIALS_PER_WINDOW) { [int]$env:TRIALS_PER_WINDOW } else { 20 }),
  [int]$PollMilliseconds = $(if ($env:POLL_MS) { [int]$env:POLL_MS } else { 100 }),
  [int]$TerminalTimeoutSeconds = $(if ($env:TERMINAL_TIMEOUT_SECONDS) { [int]$env:TERMINAL_TIMEOUT_SECONDS } else { 120 }),
  [string]$DotEnvPath = $(if ($env:DOTENV_PATH) { $env:DOTENV_PATH } else { (Join-Path $RepoRoot '.env') }),
  [string]$OutputRoot = $(if ($env:OUTPUT_ROOT) { $env:OUTPUT_ROOT } else { (Join-Path $RepoRoot '.local-run/resume-benchmark/e4-chaos') })
)

$ErrorActionPreference = 'Stop'
function Import-BenchmarkDotEnv([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { return }
  foreach ($line in Get-Content -LiteralPath $Path) {
    if ($line -match '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') {
      $name = $matches[1]
      $value = $matches[2].Trim().Trim('"').Trim("'")
      if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name)) -and -not [string]::IsNullOrWhiteSpace($value)) {
        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
      }
      if ($name -eq 'COMMERCE_RUNTIME_API_TOKEN' -and [string]::IsNullOrWhiteSpace($env:API_TOKEN) -and -not [string]::IsNullOrWhiteSpace($value)) {
        $env:API_TOKEN = $value
      }
    }
  }
}
Import-BenchmarkDotEnv $DotEnvPath
function Get-ToolVersion([string]$Name) {
  $command = Get-Command $Name -ErrorAction SilentlyContinue
  if (-not $command) { return 'UNAVAILABLE' }
  $oldPreference = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
  try { if ($Name -eq 'go') { return ((& $command.Path version 2>$null) | Out-String).Trim() }; return ((& $command.Path --version 2>$null) | Out-String).Trim() } catch { return 'UNAVAILABLE' } finally { $ErrorActionPreference = $oldPreference }
}
New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null
$windowList = @($CrashWindows -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
$gitSha = (& git -C $RepoRoot rev-parse HEAD).Trim()
$scriptHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $PSCommandPath).Hash.ToLowerInvariant()
$goVersion = Get-ToolVersion 'go'
$dockerVersion = if ($env:E4_DOCKER_SERVER_VERSION) { $env:E4_DOCKER_SERVER_VERSION } else { Get-ToolVersion 'docker' }
$manifest = [ordered]@{ benchmark = 'E4'; version = 'v1'; dataset_hash = ''; git_sha = $gitSha; runtime_source_sha = $gitSha; FROZEN_RUNTIME_CHANGED = 'NO'; seed = 42; runtime_variant = 'external-process-kill-with-deterministic-local-mocks'; workload_config_hash = "sha256:$scriptHash"; environment = [ordered]@{ os = [Environment]::OSVersion.VersionString; cpu_count = [Environment]::ProcessorCount; go_version = $goVersion; docker_version = $dockerVersion; mysql = $(if ($env:COMMERCE_RUNTIME_MYSQL_DSN) { 'configured' } else { 'not_configured' }); mysql_schema = $(if ($env:E4_MYSQL_SCHEMA) { $env:E4_MYSQL_SCHEMA } else { 'unspecified' }); mysql_container = $(if ($env:E4_MYSQL_CONTAINER_NAME) { $env:E4_MYSQL_CONTAINER_NAME } else { 'unspecified' }); mysql_image_tag = $(if ($env:E4_MYSQL_IMAGE) { $env:E4_MYSQL_IMAGE } else { 'unspecified' }); mysql_image_digest = $(if ($env:E4_MYSQL_IMAGE_DIGEST) { $env:E4_MYSQL_IMAGE_DIGEST } else { 'unspecified' }); mysql_docker_server_version = $(if ($env:E4_DOCKER_SERVER_VERSION) { $env:E4_DOCKER_SERVER_VERSION } else { 'unspecified' }); payment_adapter = 'benchmark-only MySQL-backed deterministic mock'; merchant_adapter = 'benchmark-only deterministic mock'; verification_adapter = 'benchmark-only deterministic mock'; settlement_network = 'e4-local'; rocketmq = $(if ($env:STABLEPAY_ROCKETMQ_NAME_SERVER) { 'configured' } else { 'not_configured' }); runtime_process_count = 1; service_process_count = 'recorded_by_operator'; concurrency = 1; duration = 'trial-driven' }; started_at = [DateTime]::UtcNow.ToString('o'); status = 'RUNNING' }
$manifest.environment.mock_adapter_delay_ms = $(if ($env:E4_MOCK_DELAY_MS) { [int]$env:E4_MOCK_DELAY_MS } else { 0 })
$manifest.environment.state_delay_adapter = 'benchmark-only MySQL store decorator holds DISCOVERING, INVOKING after persisted 402 response, NEGOTIATING, VALIDATING_DELIVERY and RECOVERING; production Runtime code unchanged'
$manifest.environment.invoking_crash_gate = 'authenticated benchmark-only probe confirms INITIAL 402 invocation fact is committed in E4 MySQL before the harness kills Runtime'
$manifest.environment.completion_gate = 'after terminal episode state, wait until persisted episode_execution_status is COMPLETED before stopping the restarted Runtime'
$manifest.environment.delivery_fault_injection_window = 'RECOVERING only; first delivery is empty to deterministically enter the recovery state'
$windowPlan = @($windowList | ForEach-Object { [ordered]@{ crash_window = $_; trials = $TrialsPerWindow; kill_method = 'external process Kill(); no production /debug/crash endpoint'; target_state = $_ } })
$windowPlan | ConvertTo-Json -Depth 8 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'crash_windows.json')
$trialsPath = Join-Path $OutputRoot 'trials.jsonl'
Set-Content -Encoding UTF8 -Path $trialsPath -Value ''

if ([string]::IsNullOrWhiteSpace($RuntimeExecutable) -or [string]::IsNullOrWhiteSpace($SubmitBodyPath)) {
  $manifest.status = 'NOT RUN'; $manifest.finished_at = [DateTime]::UtcNow.ToString('o'); $manifest.notes = @('RUNTIME_EXECUTABLE and SUBMIT_BODY_PATH are required; no crash claim was generated')
  $manifest | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
  @{ benchmark = 'E4'; status = 'NOT RUN'; crash_windows = $windowList; trials = 0; reason = 'runtime command or submit body unavailable'; evidence = @('manifest.json', 'crash_windows.json', 'trials.jsonl') } | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'metrics.json')
  @('# E4 Crash / Chaos Recovery Benchmark', '', '**NOT RUN**: supply an explicit Runtime executable and deterministic submit body. No recovery, TTR, or economic-correctness claim was generated.') | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'report.md')
  exit 0
}

function Invoke-JsonRequest([string]$Method, [string]$Url, $Body = $null, [string]$IdempotencyKey = '') {
  $headers = @{}
  if ($env:API_TOKEN) { $headers.Authorization = "Bearer $($env:API_TOKEN)" }
  if ($IdempotencyKey) { $headers.'Idempotency-Key' = $IdempotencyKey }
  if ($null -eq $Body) { return Invoke-RestMethod -Method $Method -Uri $Url -Headers $headers -TimeoutSec 30 }
  return Invoke-RestMethod -Method $Method -Uri $Url -Headers $headers -ContentType 'application/json' -Body ($Body | ConvertTo-Json -Depth 20) -TimeoutSec 30
}

function Wait-Ready([System.Diagnostics.Process]$Process) {
  $deadline = [DateTime]::UtcNow.AddSeconds(30)
  while ([DateTime]::UtcNow -lt $deadline) {
    if ($Process.HasExited) { throw "Runtime exited before ready: $($Process.ExitCode)" }
    try { $null = Invoke-JsonRequest 'GET' "$BaseUrl$ReadyPath"; return } catch { Start-Sleep -Milliseconds 250 }
  }
  throw 'Runtime did not become ready within 30 seconds'
}

function Start-Runtime() {
  $argumentList = @()
  if ($RuntimeArguments) { $argumentList = $RuntimeArguments -split ' ' | Where-Object { $_ } }
  if ($argumentList.Count -gt 0) {
    return Start-Process -FilePath $RuntimeExecutable -ArgumentList $argumentList -WorkingDirectory $RuntimeWorkingDirectory -PassThru -WindowStyle Hidden
  }
  return Start-Process -FilePath $RuntimeExecutable -WorkingDirectory $RuntimeWorkingDirectory -PassThru -WindowStyle Hidden
}

function Read-EpisodeId($Value) {
  if ($Value.episode.episode_id) { return [string]$Value.episode.episode_id }
  if ($Value.workflow_run.workflow_run_id) { return [string]$Value.workflow_run.workflow_run_id }
  return ''
}

function Get-State($Value) {
  if ($Value.episode.state) { return [string]$Value.episode.state }
  if ($Value.workflow_run.state) { return [string]$Value.workflow_run.state }
  return ''
}

$trialCount = 0
$successCount = 0
$ttrSeconds = @()
foreach ($window in $windowList) {
  for ($trial = 1; $trial -le $TrialsPerWindow; $trial++) {
    $trialCount++
    $env:E4_CRASH_WINDOW = $window
    $runtime = $null
    $record = [ordered]@{ trial_id = "e4-$window-$trial"; request_id = $null; task_id = $null; episode_id = $null; crash_window = $window; trial = $trial; started_at = [DateTime]::UtcNow.ToString('o'); kill_at = $null; restarted_at = $null; terminal_at = $null; persisted_target_observed = $false; durable_side_effect_observed = $false; task_completion_after_restart = $false; resume_success = $null; ttr_seconds = $null; duplicate_settlement_count = $null; duplicate_payment_intent_count = $null; duplicate_merchant_invocation_count = $null; orphaned_episode_count = $null; stuck_non_terminal_episode_count = $null; budget_drift_count = $null; artifact_provenance_error_count = $null; status = 'NOT_RUN'; error = $null }
    try {
      $runtime = Start-Runtime; Wait-Ready $runtime
      $body = Get-Content -Raw -Encoding UTF8 -LiteralPath $SubmitBodyPath | ConvertFrom-Json
      # The body file is a deterministic contract template. Every trial gets
      # a fresh logical task so a persistent database cannot turn later trials
      # into idempotent replays of the first trial.
      $trialRequestId = "e4-$window-$trial-$(Get-Date -Format 'yyyyMMddHHmmssfff')"
      $record.request_id = $trialRequestId
      $body.request_id = $trialRequestId
      if ($body.parent_session_id) { $body.parent_session_id = "$($body.parent_session_id)-$trialRequestId" }
      if ($body.input -and $body.input.uri) { $body.input.uri = "$($body.input.uri.TrimEnd('/'))/$trialRequestId" }
      $submitted = Invoke-JsonRequest 'POST' "$BaseUrl$SubmitPath" $body $trialRequestId
      $taskId = Read-EpisodeId $submitted
      if (-not $taskId) { throw 'submit response did not contain an episode or workflow id' }
      $record.task_id = $taskId; $record.episode_id = $taskId
      $targetReached = $false
      $deadline = [DateTime]::UtcNow.AddSeconds($TerminalTimeoutSeconds)
      while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-JsonRequest 'GET' ($BaseUrl + ($StatusPathTemplate -f [uri]::EscapeDataString($taskId)))
        if ((Get-State $status) -eq $window) { $targetReached = $true; break }
        Start-Sleep -Milliseconds $PollMilliseconds
      }
      if (-not $targetReached) { throw "target crash window $window was not persisted" }
      if ($window -eq 'INVOKING') {
        # INVOKING starts before the merchant response is necessarily durable.
        # Kill only after the benchmark-only authenticated probe confirms the
        # initial 402 invocation fact has been committed to MySQL.
        $durableDeadline = [DateTime]::UtcNow.AddSeconds(10)
        $durableObserved = $false
        $probeUrl = "$BaseUrl/__e4/durable-initial-invocation/$([uri]::EscapeDataString($taskId))"
        while ([DateTime]::UtcNow -lt $durableDeadline) {
          $probe = Invoke-JsonRequest 'GET' $probeUrl
          if ($probe.ready -eq $true) { $durableObserved = $true; break }
          Start-Sleep -Milliseconds $PollMilliseconds
        }
        if (-not $durableObserved) { throw 'initial 402 merchant invocation was not durably observed within 10 seconds; Runtime was not killed' }
        $record.durable_side_effect_observed = $true
      }
      $record.persisted_target_observed = $true; $record.kill_at = [DateTime]::UtcNow.ToString('o'); $runtime.Kill(); $runtime.WaitForExit()
      $runtime = Start-Runtime; Wait-Ready $runtime; $record.restarted_at = [DateTime]::UtcNow.ToString('o')
      $recoveryDeadline = [DateTime]::UtcNow.AddSeconds($TerminalTimeoutSeconds)
      $terminal = $false
      while ([DateTime]::UtcNow -lt $recoveryDeadline) {
        $status = Invoke-JsonRequest 'GET' ($BaseUrl + ($StatusPathTemplate -f [uri]::EscapeDataString($taskId)))
        $state = Get-State $status
        if (@('FULFILLED','FAILED','BLOCKED','ABORTED','EXPIRED','DISPUTED') -contains $state) {
          # The episode state and runner execution marker are separate durable
          # writes. Keep the restarted Runtime alive until both have committed.
          $executionUrl = "$BaseUrl/__e4/execution-status/$([uri]::EscapeDataString($taskId))"
          $execution = Invoke-JsonRequest 'GET' $executionUrl
          if ($execution.status -eq 'COMPLETED') { $terminal = $true; break }
        }
        Start-Sleep -Milliseconds $PollMilliseconds
      }
      $record.terminal_at = [DateTime]::UtcNow.ToString('o'); $record.task_completion_after_restart = $terminal; $record.ttr_seconds = (([DateTime]::Parse($record.terminal_at) - [DateTime]::Parse($record.restarted_at)).TotalSeconds)
      # Economic correctness must come from an explicit post-run audit. This
      # generic harness does not infer zero duplicates from a terminal state.
      if ($env:E4_AUDIT_COMMAND) { $record.audit = 'operator_configured_audit_required' }
      $record.status = $(if ($terminal) { 'COMPLETED_WITH_AUDIT_PENDING' } else { 'STUCK_NON_TERMINAL' })
      if ($terminal) { $successCount++ ; $ttrSeconds += $record.ttr_seconds }
    } catch { $record.status = 'ERROR'; $record.error = $_.Exception.Message } finally { if ($runtime -and -not $runtime.HasExited) { $runtime.Kill(); $runtime.WaitForExit() } }
    ($record | ConvertTo-Json -Compress -Depth 20) | Add-Content -Encoding UTF8 -LiteralPath $trialsPath
  }
}

$datasetHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $trialsPath).Hash.ToLowerInvariant()
$sortedTtrSeconds = @($ttrSeconds | Sort-Object)
function Get-NearestRankPercentile([double[]]$Values, [double]$Percentile) {
  if (-not $Values -or $Values.Count -eq 0) { return $null }
  $rank = [Math]::Ceiling($Percentile * $Values.Count)
  return [Math]::Round($Values[[Math]::Max(0, $rank - 1)], 3)
}
$ttrP50 = Get-NearestRankPercentile $sortedTtrSeconds 0.50
$ttrP95 = Get-NearestRankPercentile $sortedTtrSeconds 0.95
$manifest.dataset_hash = "sha256:$datasetHash"; $manifest.status = 'COMPLETE_WITH_AUDIT_PENDING'; $manifest.finished_at = [DateTime]::UtcNow.ToString('o'); $manifest.notes = @('Terminal completion is measured separately from economic correctness.', 'Duplicate settlement, duplicate PaymentIntent, budget drift, and artifact provenance require an explicit audit command.')
$manifest | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
$metrics = [ordered]@{ benchmark = 'E4'; status = $manifest.status; crash_windows = $windowList; trial_count = $trialCount; resume_success = 'AUDIT_REQUIRED'; task_completion_after_restart = @{ numerator = $successCount; denominator = $trialCount }; ttr_seconds = @{ p50 = $ttrP50; p95 = $ttrP95; method = 'nearest-rank over completed trials' }; duplicate_settlement = 'AUDIT_REQUIRED'; duplicate_payment_intent = 'AUDIT_REQUIRED'; orphaned_episodes = 'AUDIT_REQUIRED'; budget_drift = 'AUDIT_REQUIRED'; correctness_status = 'NO_RESUME_CLAIM_UNTIL_AUDIT' }
$metrics | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'metrics.json')
@('# E4 Crash / Chaos Recovery Benchmark', '', "Status: **$($manifest.status)**", '', 'FROZEN_RUNTIME_CHANGED: **NO**', '', 'Adapters: benchmark-only deterministic local merchant/payment/verification; mock payment idempotency is durable in the isolated E4 MySQL schema.', 'Controlled state dwell: benchmark-only MySQL store decorator holds DISCOVERING, INVOKING after the 402 response is persisted, NEGOTIATING, VALIDATING_DELIVERY and RECOVERING; no production Runtime package changes.', 'INVOKING kill gate: an authenticated benchmark-only probe confirms the initial 402 invocation fact is durable in MySQL before the process is killed; each trial records durable_side_effect_observed.', 'Completion gate: after the episode reaches a terminal state, the restarted Runtime remains alive until MySQL reports episode_execution_status=COMPLETED.', 'Fault injection: first delivery is empty only for trials targeting RECOVERING; other windows receive a valid first delivery.', '', "Crash windows: $($windowList -join ', ')", "Trials: $trialCount", "Task completion after restart: $successCount/$trialCount", '', 'This harness kills only the Runtime process it started, polls persisted state before killing, restarts the same command, and records raw trials. It intentionally does not convert terminal completion into a zero-duplicate economic claim. The per-episode MySQL audit is a separate required gate.', '', '## Resume Translation', '', 'Situation: A long-running task can crash after a mock payment side effect.', '', 'Task: Demonstrate durable restart recovery without duplicate economic identity.', '', 'Action: External process kill at persisted crash windows, restart, state polling, and explicit MySQL audit.', '', 'Result: Claim withheld until the full audit passes.') | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'report.md')
