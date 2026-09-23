param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [string]$BaseUrl = $(if ($env:BASE_URL) { $env:BASE_URL } else { 'http://127.0.0.1:8090' }),
  [string]$ApiToken = $(if ($env:API_TOKEN) { $env:API_TOKEN } else { '' }),
  [string]$Duration = $(if ($env:DURATION) { $env:DURATION } else { '60s' }),
  [string]$Warmup = $(if ($env:WARMUP) { $env:WARMUP } else { '15s' }),
  [string]$ConcurrencyMatrix = $(if ($env:CONCURRENCY_MATRIX) { $env:CONCURRENCY_MATRIX } else { '1,10,25,50,100,200' }),
  [string]$K6Image = $(if ($env:K6_IMAGE) { $env:K6_IMAGE } else { 'grafana/k6:latest' }),
  [string]$DockerNetworkMode = $(if ($env:K6_DOCKER_NETWORK) { $env:K6_DOCKER_NETWORK } else { 'bridge' }),
  [string]$ReadyPath = $(if ($env:READY_PATH) { $env:READY_PATH } else { '/readyz' }),
  [string]$DotEnvPath = $(if ($env:DOTENV_PATH) { $env:DOTENV_PATH } else { (Join-Path $RepoRoot '.env') }),
  [string]$OutputRoot = $(if ($env:OUTPUT_ROOT) { $env:OUTPUT_ROOT } else { (Join-Path $RepoRoot '.local-run/resume-benchmark/e3-load') })
)

$ErrorActionPreference = 'Stop'

function Get-ToolPath([string]$Name) {
  $command = Get-Command $Name -ErrorAction SilentlyContinue
  if ($command) { return $command.Source }
  if ($Name -eq 'docker') {
    $candidates = @(
      'C:\Program Files\Docker\Docker\resources\bin\docker.exe',
      'C:\Users\Administrator\AppData\Local\Programs\DockerDesktop\resources\bin\docker.exe'
    )
    foreach ($candidate in $candidates) { if (Test-Path -LiteralPath $candidate) { return $candidate } }
  }
  return $null
}

function Get-ToolVersion([string]$Name, [string]$Path) {
  if (-not $Path) { return 'UNAVAILABLE' }
  $oldPreference = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
  try {
    if ($Name -eq 'go') { return ((& $Path version 2>$null) | Out-String).Trim() }
    if ($Name -eq 'docker') { return ((& $Path version --format '{{.Server.Version}}' 2>$null) | Out-String).Trim() }
    return ((& $Path --version 2>$null) | Out-String).Trim()
  } catch { return 'UNAVAILABLE' } finally { $ErrorActionPreference = $oldPreference }
}

function Import-BenchmarkDotEnv([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) { return }
  foreach ($line in Get-Content -LiteralPath $Path) {
    if ($line -match '^\s*(API_TOKEN|COMMERCE_RUNTIME_API_TOKEN|BASE_URL|COMMERCE_RUNTIME_MYSQL_DSN|COMMERCE_RUNTIME_MYSQL_MAX_OPEN_CONNS|COMMERCE_RUNTIME_MYSQL_MAX_IDLE_CONNS|COMMERCE_RUNTIME_MYSQL_MAX_IDLE_TIME|COMMERCE_RUNTIME_MYSQL_MAX_LIFETIME)\s*=\s*(.*)\s*$') {
      $name = $matches[1]
      $value = $matches[2].Trim().Trim('"').Trim("'")
      if (-not [string]::IsNullOrWhiteSpace($value)) {
        if ($name -eq 'COMMERCE_RUNTIME_API_TOKEN' -and [string]::IsNullOrWhiteSpace($env:API_TOKEN)) { $env:API_TOKEN = $value }
        elseif ($name -eq 'API_TOKEN' -and [string]::IsNullOrWhiteSpace($env:API_TOKEN)) { $env:API_TOKEN = $value }
        elseif ($name -eq 'BASE_URL' -and [string]::IsNullOrWhiteSpace($env:BASE_URL)) { $env:BASE_URL = $value }
        elseif ($name -eq 'COMMERCE_RUNTIME_MYSQL_DSN' -and [string]::IsNullOrWhiteSpace($env:COMMERCE_RUNTIME_MYSQL_DSN)) { $env:COMMERCE_RUNTIME_MYSQL_DSN = $value }
        elseif ($name -like 'COMMERCE_RUNTIME_MYSQL_MAX_*' -and [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) { Set-Item -Path ("Env:" + $name) -Value $value }
      }
    }
  }
}

function Get-ContainerBaseUrl([string]$Url) {
  $value = $Url.TrimEnd('/')
  if ($value -match '^http://(127\.0\.0\.1|localhost)(?::\d+)?(?:/.*)?$') {
    return ($value -replace '^http://(127\.0\.0\.1|localhost)', 'http://host.docker.internal')
  }
  return $value
}

function Get-ImageDigest([string]$DockerPath, [string]$Image) {
  $raw = (& $DockerPath image inspect $Image --format '{{json .RepoDigests}}' 2>$null | Out-String).Trim()
  if ([string]::IsNullOrWhiteSpace($raw)) { return '' }
  try {
    $digests = $raw | ConvertFrom-Json
    if ($digests -is [System.Array]) { return [string]($digests | Select-Object -First 1) }
    return [string]$digests
  } catch { return '' }
}

function Get-SummaryMetricValue([object]$Summary, [string]$MetricName, [string]$Field, [double]$Default = 0) {
  $metricProperty = $Summary.metrics.PSObject.Properties[$MetricName]
  if (-not $metricProperty) { return $Default }
  $fieldProperty = $metricProperty.Value.PSObject.Properties[$Field]
  if (-not $fieldProperty -or $null -eq $fieldProperty.Value) { return $Default }
  try { return [double]$fieldProperty.Value } catch { return $Default }
}

function ConvertTo-DurationSeconds([string]$Value) {
  if ($Value -notmatch '^(?<amount>\d+)(?<unit>ms|s|m|h)$') { throw "unsupported duration format: $Value" }
  $amount = [double]$matches.amount
  switch ($matches.unit) {
    'ms' { return $amount / 1000 }
    's' { return $amount }
    'm' { return $amount * 60 }
    'h' { return $amount * 3600 }
  }
}

function Write-NotRun([System.Collections.IDictionary]$Manifest, [string]$Reason) {
  $Manifest.status = 'NOT RUN'
  $Manifest.finished_at = [DateTime]::UtcNow.ToString('o')
  $Manifest.notes = @($Reason)
  $Manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
  @{ benchmark = 'E3'; status = 'NOT RUN'; reason = $Reason; evidence = @('manifest.json', 'metrics.json') } |
    ConvertTo-Json -Depth 8 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'metrics.json')
  @('# E3 Runtime / CloudWeGo Load Benchmark', '', "**NOT RUN**: $Reason") |
    Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'report.md')
}

Import-BenchmarkDotEnv $DotEnvPath
if ([string]::IsNullOrWhiteSpace($ApiToken) -and $env:API_TOKEN) { $ApiToken = $env:API_TOKEN }
if ([string]::IsNullOrWhiteSpace($BaseUrl) -and $env:BASE_URL) { $BaseUrl = $env:BASE_URL }

$scriptPath = Join-Path $RepoRoot 'benchmarks/e3-load/runtime_control_plane.js'
$dockerPath = Get-ToolPath 'docker'
$goPath = Get-ToolPath 'go'
New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null
$started = [DateTime]::UtcNow
$gitSha = (& git -C $RepoRoot rev-parse HEAD).Trim()
$configHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $scriptPath).Hash.ToLowerInvariant()
$goVersion = Get-ToolVersion 'go' $goPath
$dockerVersion = Get-ToolVersion 'docker' $dockerPath
$containerBaseUrl = Get-ContainerBaseUrl $BaseUrl
$manifest = [ordered]@{
  benchmark = 'E3'; version = 'v1'; dataset_hash = ''; git_sha = $gitSha
  runtime_source_sha = $gitSha; seed = 42
  runtime_variant = 'real-runtime-deterministic-local-dependencies'; workload_config_hash = "sha256:$configHash"
  k6_image = $K6Image; k6_image_tag = ''; k6_image_digest = ''; docker_network_mode = $DockerNetworkMode
  environment = [ordered]@{
    os = [System.Environment]::OSVersion.VersionString; cpu_count = [Environment]::ProcessorCount
    go_version = $goVersion; docker_version = $dockerVersion
    mysql = $(if ($env:COMMERCE_RUNTIME_MYSQL_DSN) { 'configured' } else { 'not_configured' })
    mysql_pool = [ordered]@{
      max_open_connections = $(if ($env:COMMERCE_RUNTIME_MYSQL_MAX_OPEN_CONNS) { $env:COMMERCE_RUNTIME_MYSQL_MAX_OPEN_CONNS } else { '100' })
      max_idle_connections = $(if ($env:COMMERCE_RUNTIME_MYSQL_MAX_IDLE_CONNS) { $env:COMMERCE_RUNTIME_MYSQL_MAX_IDLE_CONNS } else { '80' })
      max_idle_time = $(if ($env:COMMERCE_RUNTIME_MYSQL_MAX_IDLE_TIME) { $env:COMMERCE_RUNTIME_MYSQL_MAX_IDLE_TIME } else { '5m' })
      max_lifetime = $(if ($env:COMMERCE_RUNTIME_MYSQL_MAX_LIFETIME) { $env:COMMERCE_RUNTIME_MYSQL_MAX_LIFETIME } else { '30m' })
    }
    rocketmq = $(if ($env:STABLEPAY_ROCKETMQ_NAME_SERVER) { $env:STABLEPAY_ROCKETMQ_NAME_SERVER } else { 'not_configured' })
    runtime_process_count = 'recorded_by_operator'; service_process_count = 'recorded_by_operator'
    concurrency = $ConcurrencyMatrix; warmup = $Warmup; duration = $Duration
  }
  started_at = $started.ToString('o'); status = 'RUNNING'
}
$manifest.environment.container_base_url = $containerBaseUrl
$baseNotes = @($manifest.notes | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) })

if ($K6Image -match '^(?<name>[^@]+?)(?::(?<tag>[^:]+))?$') { $manifest.k6_image_tag = $(if ($matches.tag) { $matches.tag } else { 'latest' }) }
if (-not $dockerPath) { Write-NotRun $manifest 'Docker CLI is unavailable; Docker Desktop must be installed and reachable'; exit 0 }

try {
  & $dockerPath pull $K6Image | Tee-Object -FilePath (Join-Path $OutputRoot 'docker-pull.log')
  if ($LASTEXITCODE -ne 0) { throw "docker pull failed for $K6Image" }
  $manifest.k6_image_digest = Get-ImageDigest $dockerPath $K6Image
  if ([string]::IsNullOrWhiteSpace($manifest.k6_image_digest)) { throw "could not resolve RepoDigest for $K6Image" }
} catch {
  $manifest.k6_image_digest = Get-ImageDigest $dockerPath $K6Image
  if ([string]::IsNullOrWhiteSpace($manifest.k6_image_digest)) {
    Write-NotRun $manifest $_.Exception.Message
    exit 0
  }
  $manifest.notes = @("docker pull unavailable; using cached image $($manifest.k6_image_digest)")
}

try {
  $ready = $null
  $readyError = ''
  for ($attempt = 1; $attempt -le 20; $attempt++) {
    try {
      $probe = Invoke-WebRequest -UseBasicParsing -Uri ($BaseUrl.TrimEnd('/') + $ReadyPath) -TimeoutSec 5
      if ($probe.StatusCode -ge 200 -and $probe.StatusCode -lt 300) { $ready = $probe; break }
      $readyError = "Runtime readiness returned HTTP $($probe.StatusCode)"
    } catch { $readyError = $_.Exception.Message }
    Start-Sleep -Milliseconds 250
  }
  if (-not $ready) { throw $readyError }
} catch {
  Write-NotRun $manifest "Runtime readiness probe failed at ${BaseUrl}${ReadyPath}: $($_.Exception.Message)"
  exit 0
}

$rows = @()
$runFailures = @()
foreach ($concurrency in ($ConcurrencyMatrix -split ',' | ForEach-Object { [int]$_.Trim() })) {
  $dir = Join-Path $OutputRoot ("k6/concurrency-{0}" -f $concurrency)
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $summary = '/out/summary.json'
  $hostDir = (Resolve-Path -LiteralPath $dir).Path
  $hostScript = (Resolve-Path -LiteralPath $scriptPath).Path
  $dockerArgs = @(
    'run', '--rm', '--network', $DockerNetworkMode,
    '-e', "BASE_URL=$containerBaseUrl", '-e', "CONCURRENCY=$concurrency", '-e', "DURATION=$Duration", '-e', "WARMUP=$Warmup",
    '-e', 'API_TOKEN', '-v', "${hostScript}:/scripts/runtime_control_plane.js:ro", '-v', "${hostDir}:/out",
    $K6Image, 'run', '--quiet', '--log-output=none', "--summary-export=$summary", '/scripts/runtime_control_plane.js'
  )
  if ($ApiToken) { $env:API_TOKEN = $ApiToken }
  $k6ExitCode = -1
  $previousPreference = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try {
    & $dockerPath @dockerArgs 2>&1 | Tee-Object -FilePath (Join-Path $dir 'k6.log')
    $k6ExitCode = $LASTEXITCODE
  } catch {
    $runFailures += "concurrency $concurrency runner exception: $($_.Exception.Message)"
  } finally {
    $ErrorActionPreference = $previousPreference
  }
  $summaryPath = Join-Path $dir 'summary.json'
  $thresholdsFailed = $k6ExitCode -ne 0
  if ($thresholdsFailed) {
    $runFailures += "concurrency $concurrency exited with k6 code $k6ExitCode; inspect k6/concurrency-$concurrency/summary.json"
  }
  if (-not (Test-Path -LiteralPath $summaryPath -PathType Leaf)) {
    $runFailures += "concurrency $concurrency produced no summary.json"
  }
  $measurement = $null
  if (Test-Path -LiteralPath $summaryPath -PathType Leaf) {
    try {
      $summaryData = Get-Content -LiteralPath $summaryPath -Raw | ConvertFrom-Json
      $measurementSeconds = ConvertTo-DurationSeconds $Duration
      $measuredIterations = Get-SummaryMetricValue $summaryData 'measurement_iterations' 'count'
      $measuredHTTPRequests = Get-SummaryMetricValue $summaryData 'measurement_http_requests' 'count'
      $measuredCompletedEpisodes = Get-SummaryMetricValue $summaryData 'measurement_completed_episodes' 'count'
      $measuredCreateStatuses = [ordered]@{}
      $measuredPollStatuses = [ordered]@{}
      foreach ($statusCode in @(200, 201, 202, 400, 401, 404, 409, 422, 500, 502, 503, 504)) {
        $measuredCreateStatuses[[string]$statusCode] = Get-SummaryMetricValue $summaryData "measurement_create_status_$statusCode" 'count'
        $measuredPollStatuses[[string]$statusCode] = Get-SummaryMetricValue $summaryData "measurement_status_poll_status_$statusCode" 'count'
      }
      $measuredCreateStatuses.other = Get-SummaryMetricValue $summaryData 'measurement_create_status_other' 'count'
      $measuredPollStatuses.other = Get-SummaryMetricValue $summaryData 'measurement_status_poll_status_other' 'count'
      $measurement = [ordered]@{
        window_seconds = $measurementSeconds
        iterations = $measuredIterations
        iterations_per_second = [Math]::Round($measuredIterations / $measurementSeconds, 3)
        http_requests = $measuredHTTPRequests
        http_requests_per_second = [Math]::Round($measuredHTTPRequests / $measurementSeconds, 3)
        completed_episodes = $measuredCompletedEpisodes
        completed_episodes_per_second = [Math]::Round($measuredCompletedEpisodes / $measurementSeconds, 3)
        business_errors = Get-SummaryMetricValue $summaryData 'measurement_business_errors' 'count'
        business_error_rate = Get-SummaryMetricValue $summaryData 'business_error_rate{phase:measurement}' 'value'
        collection_errors = Get-SummaryMetricValue $summaryData 'measurement_collection_errors' 'count'
        orphaned_episodes = Get-SummaryMetricValue $summaryData 'measurement_orphaned_episodes' 'count'
        http_req_failed_rate = Get-SummaryMetricValue $summaryData 'http_req_failed{phase:measurement}' 'value'
        create_latency_p95_ms = Get-SummaryMetricValue $summaryData 'measurement_create_latency' 'p(95)'
        status_latency_p95_ms = Get-SummaryMetricValue $summaryData 'measurement_status_latency' 'p(95)'
        http_req_duration_p95_ms = Get-SummaryMetricValue $summaryData 'measurement_http_request_latency' 'p(95)'
        episode_e2e_latency_p95_ms = Get-SummaryMetricValue $summaryData 'measurement_episode_e2e_latency' 'p(95)'
        create_status_counts = $measuredCreateStatuses
        status_poll_status_counts = $measuredPollStatuses
      }
    } catch {
      $runFailures += "concurrency $concurrency could not extract measurement metrics: $($_.Exception.Message)"
    }
  }
  $rows += [ordered]@{ concurrency = $concurrency; warmup = $Warmup; duration = $Duration; exit_code = $k6ExitCode; thresholds_failed = $thresholdsFailed; measurement = $measurement; summary = "k6/concurrency-$concurrency/summary.json" }
  $manifest.status = 'RUNNING'
  $manifest.matrix = $rows
  $manifest.notes = @($baseNotes) + @($runFailures)
  $manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
}

$manifest.status = $(if ($runFailures.Count -gt 0) { 'COMPLETE_WITH_THRESHOLD_FAILURES' } else { 'COMPLETE' }); $manifest.finished_at = [DateTime]::UtcNow.ToString('o')
$manifest.environment.concurrency = $ConcurrencyMatrix
$manifest.environment.container_base_url = $containerBaseUrl
$manifest.notes = @($baseNotes) + @($runFailures)
$manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
$metrics = [ordered]@{
  benchmark = 'E3'; status = $manifest.status; runtime_variant = $manifest.runtime_variant
  http_api_throughput = 'see k6 summaries'; business_episode_completion_throughput = 'see completed_episodes metric'
  matrix = $rows; docker = [ordered]@{ image = $K6Image; tag = $manifest.k6_image_tag; digest = $manifest.k6_image_digest; network_mode = $DockerNetworkMode }
  duplicate_settlement_count = 'requires post-run ledger audit'; orphaned_episode_count = 'requires post-run Runtime audit'
  limitations = @('deterministic local dependencies', 'E3B CloudWeGo target is not claimed by E3A')
}
$metrics | ConvertTo-Json -Depth 14 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'metrics.json')
$reportLines = @(
  '# E3 Runtime / CloudWeGo Load Benchmark', '',
  "Status: **$($manifest.status)**", '',
  "Runtime source SHA: $($manifest.runtime_source_sha)",
  "Docker image: $K6Image", "Digest: $($manifest.k6_image_digest)",
  "Network mode: $DockerNetworkMode", '',
  "Steady-state metrics below cover only the $Duration measurement window after the $Warmup warmup.", '',
  '| VUs | HTTP req/s | Completed episodes/s | HTTP p95 (ms) | Episode p95 (ms) | HTTP errors | Business errors |',
  '|---:|---:|---:|---:|---:|---:|---:|'
)
foreach ($row in $rows) {
  if ($null -eq $row.measurement) { continue }
  $httpErrorPct = [Math]::Round([double]$row.measurement.http_req_failed_rate * 100, 2)
  $businessErrorPct = [Math]::Round([double]$row.measurement.business_error_rate * 100, 2)
  $reportLines += "| $($row.concurrency) | $($row.measurement.http_requests_per_second) | $($row.measurement.completed_episodes_per_second) | $($row.measurement.http_req_duration_p95_ms) | $($row.measurement.episode_e2e_latency_p95_ms) | $httpErrorPct% | $businessErrorPct% |"
}
$reportLines += @(
  '',
  'HTTP API throughput and completed business episode throughput are reported separately. Each raw k6 summary is under `k6/`.', '',
  'A non-zero k6 exit records a threshold failure for that tier but does not prevent remaining matrix tiers from running.', '',
  'E3A uses the real Runtime HTTP boundary with deterministic local dependencies. Duplicate settlement still requires a post-run ledger audit.'
)
$reportLines | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'report.md')
