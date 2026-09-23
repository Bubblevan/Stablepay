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
    if ($line -match '^\s*(API_TOKEN|COMMERCE_RUNTIME_API_TOKEN|BASE_URL)\s*=\s*(.*)\s*$') {
      $name = $matches[1]
      $value = $matches[2].Trim().Trim('"').Trim("'")
      if (-not [string]::IsNullOrWhiteSpace($value)) {
        if ($name -eq 'COMMERCE_RUNTIME_API_TOKEN' -and [string]::IsNullOrWhiteSpace($env:API_TOKEN)) { $env:API_TOKEN = $value }
        elseif ($name -eq 'API_TOKEN' -and [string]::IsNullOrWhiteSpace($env:API_TOKEN)) { $env:API_TOKEN = $value }
        elseif ($name -eq 'BASE_URL' -and [string]::IsNullOrWhiteSpace($env:BASE_URL)) { $env:BASE_URL = $value }
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
  frozen_runtime_sha = 'ae67ae5e48a76848b5c0dfc4a68f79eef00a5705'; seed = 42
  runtime_variant = 'real-runtime-deterministic-local-dependencies'; workload_config_hash = "sha256:$configHash"
  k6_image = $K6Image; k6_image_tag = ''; k6_image_digest = ''; docker_network_mode = $DockerNetworkMode
  environment = [ordered]@{
    os = [System.Environment]::OSVersion.VersionString; cpu_count = [Environment]::ProcessorCount
    go_version = $goVersion; docker_version = $dockerVersion
    mysql = $(if ($env:COMMERCE_RUNTIME_MYSQL_DSN) { 'configured' } else { 'not_configured' })
    rocketmq = $(if ($env:STABLEPAY_ROCKETMQ_NAME_SERVER) { $env:STABLEPAY_ROCKETMQ_NAME_SERVER } else { 'not_configured' })
    runtime_process_count = 'recorded_by_operator'; service_process_count = 'recorded_by_operator'
    concurrency = $ConcurrencyMatrix; warmup = $Warmup; duration = $Duration
  }
  started_at = $started.ToString('o'); status = 'RUNNING'
}
$manifest.environment.container_base_url = $containerBaseUrl

if ($K6Image -match '^(?<name>[^@]+?)(?::(?<tag>[^:]+))?$') { $manifest.k6_image_tag = $(if ($matches.tag) { $matches.tag } else { 'latest' }) }
if (-not $dockerPath) { Write-NotRun $manifest 'Docker CLI is unavailable; Docker Desktop must be installed and reachable'; exit 0 }

try {
  & $dockerPath pull $K6Image | Tee-Object -FilePath (Join-Path $OutputRoot 'docker-pull.log')
  if ($LASTEXITCODE -ne 0) { throw "docker pull failed for $K6Image" }
  $manifest.k6_image_digest = Get-ImageDigest $dockerPath $K6Image
  if ([string]::IsNullOrWhiteSpace($manifest.k6_image_digest)) { throw "could not resolve RepoDigest for $K6Image" }
} catch {
  Write-NotRun $manifest $_.Exception.Message
  exit 0
}

try {
  $ready = Invoke-WebRequest -UseBasicParsing -Uri ($BaseUrl.TrimEnd('/') + $ReadyPath) -TimeoutSec 5
  if ($ready.StatusCode -lt 200 -or $ready.StatusCode -ge 300) { throw "Runtime readiness returned HTTP $($ready.StatusCode)" }
} catch {
  Write-NotRun $manifest "Runtime readiness probe failed at ${BaseUrl}${ReadyPath}: $($_.Exception.Message)"
  exit 0
}

$rows = @()
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
    $K6Image, 'run', "--summary-export=$summary", '/scripts/runtime_control_plane.js'
  )
  if ($ApiToken) { $env:API_TOKEN = $ApiToken }
  & $dockerPath @dockerArgs 2>&1 | Tee-Object -FilePath (Join-Path $dir 'k6.log')
  if ($LASTEXITCODE -ne 0) { throw "Docker k6 failed at concurrency $concurrency" }
  $rows += [ordered]@{ concurrency = $concurrency; warmup = $Warmup; duration = $Duration; summary = "k6/concurrency-$concurrency/summary.json" }
}

$manifest.status = 'COMPLETE'; $manifest.finished_at = [DateTime]::UtcNow.ToString('o')
$manifest.environment.concurrency = $ConcurrencyMatrix
$manifest.environment.container_base_url = $containerBaseUrl
$manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
$metrics = [ordered]@{
  benchmark = 'E3'; status = 'COMPLETE'; runtime_variant = $manifest.runtime_variant
  http_api_throughput = 'see k6 summaries'; business_episode_completion_throughput = 'see completed_episodes metric'
  matrix = $rows; docker = [ordered]@{ image = $K6Image; tag = $manifest.k6_image_tag; digest = $manifest.k6_image_digest; network_mode = $DockerNetworkMode }
  duplicate_settlement_count = 'requires post-run ledger audit'; orphaned_episode_count = 'requires post-run Runtime audit'
  limitations = @('deterministic local dependencies', 'E3B CloudWeGo target is not claimed by E3A')
}
$metrics | ConvertTo-Json -Depth 14 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'metrics.json')
@('# E3 Runtime / CloudWeGo Load Benchmark', '', "Status: **$($manifest.status)**", '', "Docker image: $K6Image", "Digest: $($manifest.k6_image_digest)", "Network mode: $DockerNetworkMode", '', 'HTTP API throughput and business episode completion throughput are reported separately. Review each k6 summary under `k6/` before making a resume claim.', '', 'E3A uses the real Runtime HTTP boundary and deterministic local dependencies. Duplicate settlement and orphaned episode counts require the post-run ledger/status audit and are not inferred from request QPS.') |
  Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'report.md')
