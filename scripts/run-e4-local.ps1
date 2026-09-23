[CmdletBinding()]
param(
  [string]$RepoRoot = '',
  [string]$OutputRoot = '',
  [string]$CrashWindows = 'DISCOVERING,INVOKING,NEGOTIATING,PAYING,CLAIMING,INVOKING_DELIVERY,VALIDATING_DELIVERY,RECOVERING',
  [string]$SmokeWindow = 'PAYING',
  [int]$TrialsPerWindow = 20,
  [int]$PollMilliseconds = 5,
  [string]$DockerMySQLImage = 'mysql:8.0',
  [int]$MySQLHostPort = 13308,
  [string]$ReuseMySQLContainerName = ''
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
  $RepoRoot = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
}
$runtimeRoot = Join-Path $RepoRoot 'commerce-runtime'
$dotenv = Join-Path $RepoRoot '.env'
$e4Script = Join-Path $RepoRoot 'scripts\benchmark-e4-chaos.ps1'
$bodyPath = Join-Path $RepoRoot 'testdata\benchmark\e4-submit-body.json'

function Import-BenchmarkDotEnv([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { throw "Missing dotenv file: $Path" }
  $allowed = @('COMMERCE_RUNTIME_API_TOKEN')
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

function Invoke-Go([string[]]$Arguments, [switch]$Quiet) {
  Push-Location $runtimeRoot
  try {
    if ($Quiet) { & go @Arguments *> $null } else { & go @Arguments }
    if ($LASTEXITCODE -ne 0) { throw "go $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
  } finally { Pop-Location }
}

function Invoke-Docker([string[]]$Arguments, [switch]$Quiet) {
  if ($Quiet) { $result = & $dockerPath @Arguments *> $null } else { $result = & $dockerPath @Arguments }
  if ($LASTEXITCODE -ne 0) { throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
  return $result
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
if ([string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_API_TOKEN)) {
  throw '.env must define COMMERCE_RUNTIME_API_TOKEN'
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
$mysqlPortInUse = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -eq $MySQLHostPort }
if ($mysqlPortInUse -and -not $ReuseMySQLContainerName) { throw "E4 isolated MySQL host port $MySQLHostPort is already in use; refusing to attach to an unrelated database" }

$dockerCommand = Get-Command docker -ErrorAction SilentlyContinue
if ($dockerCommand) {
  $dockerPath = $dockerCommand.Source
} else {
  $dockerPath = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources\bin\docker.exe'
}
if (-not (Test-Path -LiteralPath $dockerPath)) { throw 'Docker CLI is unavailable; cannot start the E4-only MySQL container' }
$dockerVersion = (Invoke-Docker @('version', '--format', '{{.Server.Version}}')).Trim()
$imageId = (Invoke-Docker @('image', 'inspect', $DockerMySQLImage, '--format', '{{.Id}}')).Trim()
$repoDigestsJson = (Invoke-Docker @('image', 'inspect', $DockerMySQLImage, '--format', '{{json .RepoDigests}}')).Trim()
$repoDigests = @($repoDigestsJson | ConvertFrom-Json)
$imageDigest = if ($repoDigests.Count -gt 0) { [string]$repoDigests[0] } else { $imageId }
$stamp = [DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
$schemaSuffix = Get-Random -Minimum 1000 -Maximum 9999
if ($ReuseMySQLContainerName) {
  $containerJson = (Invoke-Docker @('inspect', '--format', '{{json .}}', $ReuseMySQLContainerName) | Out-String).Trim()
  $container = ConvertFrom-Json -InputObject $containerJson
  if ($container.State.Status -ne 'running' -or $container.Config.Image -ne $DockerMySQLImage) {
    throw 'Requested E4 MySQL container is not running the expected image tag'
  }
  $containerEnv = @{}
  foreach ($entry in $container.Config.Env) {
    $pair = $entry -split '=', 2
    if ($pair.Count -eq 2) { $containerEnv[$pair[0]] = $pair[1] }
  }
  $schema = $containerEnv.MYSQL_DATABASE
  $appUser = $containerEnv.MYSQL_USER
  $appPassword = $containerEnv.MYSQL_PASSWORD
  if ($schema -notmatch '^stablepay_e4_[a-z0-9_]{1,48}$' -or -not $appUser -or -not $appPassword) {
    throw 'Requested container does not contain a valid isolated E4 schema and app account'
  }
  $publishedPorts = @($container.NetworkSettings.Ports.'3306/tcp')
  if ($publishedPorts.Count -ne 1 -or $publishedPorts[0].HostIp -ne '127.0.0.1' -or [int]$publishedPorts[0].HostPort -ne $MySQLHostPort) {
    throw 'Requested MySQL container is not bound exclusively to the expected loopback port'
  }
  $containerName = $container.Name.TrimStart('/')
  $volumeMount = @($container.Mounts | Where-Object { $_.Destination -eq '/var/lib/mysql' }) | Select-Object -First 1
  if (-not $volumeMount -or $volumeMount.Type -ne 'volume') { throw 'Requested E4 MySQL container does not have its expected named data volume' }
  $volumeName = $volumeMount.Name
} else {
  $schema = 'stablepay_e4_' + $stamp + '_' + $schemaSuffix
  $containerName = 'stablepay-e4-mysql-' + $stamp + '-' + $schemaSuffix
  $volumeName = 'stablepay-e4-data-' + $stamp + '-' + $schemaSuffix
  $appUser = 'e4bench'
  $appPassword = [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N')
  $rootPassword = [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N')
  $dockerRunArguments = @(
    'run', '--detach', '--pull=never', '--name', $containerName,
    '--publish', "127.0.0.1:${MySQLHostPort}:3306",
    '--volume', "${volumeName}:/var/lib/mysql",
    '--env', "MYSQL_DATABASE=$schema",
    '--env', "MYSQL_USER=$appUser",
    '--env', "MYSQL_PASSWORD=$appPassword",
    '--env', "MYSQL_ROOT_PASSWORD=$rootPassword",
    $DockerMySQLImage
  )
  $null = Invoke-Docker $dockerRunArguments
}
$newDsn = "${appUser}:$($appPassword)@tcp(127.0.0.1:$MySQLHostPort)/${schema}?parseTime=true&loc=UTC"
$env:E4_MYSQL_SCHEMA = $schema
$env:E4_MYSQL_CONTAINER_NAME = $containerName
$env:E4_MYSQL_IMAGE = $DockerMySQLImage
$env:E4_MYSQL_IMAGE_DIGEST = $imageDigest
$env:E4_DOCKER_SERVER_VERSION = $dockerVersion
$env:COMMERCE_RUNTIME_MYSQL_DSN = $newDsn
$schemaReady = $false
for ($attempt = 1; $attempt -le 60; $attempt++) {
  try {
    Invoke-Go @('run', './cmd/e4-mysql-audit', 'check-schema', '-name', $schema) -Quiet
    $schemaReady = $true
    break
  } catch {
    if (($attempt % 10) -eq 0) { Write-Host "Waiting for isolated MySQL readiness ($attempt/60)..." }
    Start-Sleep -Seconds 2
  }
}
if (-not $schemaReady) { throw "Dedicated MySQL container did not initialize schema $schema within 120 seconds; container $containerName was retained for inspection" }

$runtimeExe = Join-Path $OutputRoot 'commerce-runtime-e4-local.exe'
Invoke-Go @('build', '-trimpath', '-o', $runtimeExe, './cmd/e4-local-runtime')

$setup = [ordered]@{
  benchmark = 'E4'
  mode = 'benchmark-only deterministic local adapters'
  mysql_schema = $schema
  mysql_backend = 'dedicated Docker container; original MySQL DSN not used'
  mysql_container = $containerName
  mysql_volume = $volumeName
  mysql_host = "127.0.0.1:$MySQLHostPort"
  mysql_image_tag = $DockerMySQLImage
  mysql_image_digest = $imageDigest
  docker_server_version = $dockerVersion
  settlement_network = 'e4-local'
  payment_adapter = 'MySQL-backed idempotent mock; no chain client'
  state_window_delay_ms = 150
  state_delay_adapter = 'benchmark-only MySQL store decorator for DISCOVERING, INVOKING after persisted 402 response, NEGOTIATING, VALIDATING_DELIVERY, RECOVERING'
  terminal_execution_reconciler = 'benchmark-only loop calls the existing Runner.ResumeEpisode for terminal episodes with RUNNING execution markers; production Runtime code unchanged'
  delivery_fault_injection_window = 'RECOVERING only'
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
