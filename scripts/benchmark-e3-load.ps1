param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [string]$BaseUrl = $(if ($env:BASE_URL) { $env:BASE_URL } else { 'http://127.0.0.1:8090' }),
  [string]$ApiToken = $(if ($env:API_TOKEN) { $env:API_TOKEN } else { '' }),
  [string]$Duration = $(if ($env:DURATION) { $env:DURATION } else { '60s' }),
  [string]$Warmup = $(if ($env:WARMUP) { $env:WARMUP } else { '15s' }),
  [string]$ConcurrencyMatrix = $(if ($env:CONCURRENCY_MATRIX) { $env:CONCURRENCY_MATRIX } else { '1,10,25,50,100,200' }),
  [string]$K6Image = $(if ($env:K6_IMAGE) { $env:K6_IMAGE } else { 'grafana/k6:latest' }),
  [switch]$PullK6Image,
  [switch]$ConfirmDeterministicLocalDependencies,
  [switch]$StopOnThresholdFailure,
  [ValidateSet('host', 'isolated-kubernetes', 'unknown')]
  [string]$RuntimeDatabasePlacement = $(if ($env:RUNTIME_DATABASE_PLACEMENT) { $env:RUNTIME_DATABASE_PLACEMENT } else { 'host' }),
  [string]$KubernetesProfile = $(if ($env:KUBERNETES_PROFILE) { $env:KUBERNETES_PROFILE } else { '' }),
  [string]$DockerNetworkMode = $(if ($env:K6_DOCKER_NETWORK) { $env:K6_DOCKER_NETWORK } else { 'bridge' }),
  [string]$ReadyPath = $(if ($env:READY_PATH) { $env:READY_PATH } else { '/readyz' }),
  [string]$DotEnvPath = $(if ($env:DOTENV_PATH) { $env:DOTENV_PATH } else { (Join-Path $RepoRoot '.env') }),
  [string]$OutputRoot = $(if ($env:OUTPUT_ROOT) { $env:OUTPUT_ROOT } else { (Join-Path $RepoRoot '.local-run/resume-benchmark/e3-load') })
)

$ErrorActionPreference = 'Stop'

if (-not $ConfirmDeterministicLocalDependencies) {
  throw 'Refusing E3: every iteration creates a new Episode, and /readyz does not report payment mode. Verify the target uses deterministic local dependencies with no Devnet/provider side effects, then pass -ConfirmDeterministicLocalDependencies.'
}

$OutputRoot = [System.IO.Path]::GetFullPath($OutputRoot)
if (Test-Path -LiteralPath $OutputRoot) {
  if (-not (Test-Path -LiteralPath $OutputRoot -PathType Container)) {
    throw "E3 output path exists and is not a directory: $OutputRoot"
  }
  $existingOutput = Get-ChildItem -LiteralPath $OutputRoot -Force | Select-Object -First 1
  if ($existingOutput) {
    throw "E3 output directory is not empty; refusing to overwrite evidence: $OutputRoot. Choose a new OutputRoot."
  }
}

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

function Get-SummaryMetricValue([object]$Summary, [string]$MetricName, [string]$Field, [object]$Default = $null) {
  $metricProperty = $Summary.metrics.PSObject.Properties[$MetricName]
  if (-not $metricProperty) {
    if ($null -ne $Default) { return [double]$Default }
    throw "k6 summary is missing metric $MetricName"
  }
  $fieldProperty = $metricProperty.Value.PSObject.Properties[$Field]
  if (-not $fieldProperty -or $null -eq $fieldProperty.Value) {
    if ($null -ne $Default) { return [double]$Default }
    throw "k6 summary is missing $MetricName.$Field"
  }
  try { return [double]$fieldProperty.Value } catch {
    if ($null -ne $Default) { return [double]$Default }
    throw "k6 summary has a non-numeric value for $MetricName.$Field"
  }
}

function Get-K6ThresholdFailures([object]$Summary) {
  $failures = @()
  if (-not $Summary -or -not $Summary.metrics) { return $failures }
  foreach ($metric in $Summary.metrics.PSObject.Properties) {
    $thresholdsProperty = $metric.Value.PSObject.Properties['thresholds']
    if (-not $thresholdsProperty -or -not $thresholdsProperty.Value) { continue }
    foreach ($threshold in $thresholdsProperty.Value.PSObject.Properties) {
      if ($threshold.Value -isnot [bool]) {
        throw "k6 threshold status is not boolean for $($metric.Name):$($threshold.Name)"
      }
      if ($threshold.Value) {
        $failures += "$($metric.Name):$($threshold.Name)"
      }
    }
  }
  return $failures
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

function Convert-MemorySizeToBytes([string]$Value) {
  if ($Value -notmatch '^\s*(?<amount>\d+(?:\.\d+)?)\s*(?<unit>B|KiB|MiB|GiB|TiB)\s*$') {
    throw "unrecognized Docker memory size: $Value"
  }
  $amount = [double]::Parse($matches.amount, [Globalization.CultureInfo]::InvariantCulture)
  $factor = switch ($matches.unit) {
    'B' { 1.0 }
    'KiB' { 1KB }
    'MiB' { 1MB }
    'GiB' { 1GB }
    'TiB' { 1TB }
  }
  return [double]($amount * $factor)
}

function Get-KubernetesContainerCgroupMemory([string]$KubectlPath, [string]$Namespace,
  [string]$DeploymentName, [string]$ContainerName) {
  $probe = 'if [ -r /sys/fs/cgroup/memory.current ] && [ -r /sys/fs/cgroup/memory.max ]; then used=$(cat /sys/fs/cgroup/memory.current); limit=$(cat /sys/fs/cgroup/memory.max); inactive=$(grep ''^inactive_file '' /sys/fs/cgroup/memory.stat | cut -d '' '' -f2); elif [ -r /sys/fs/cgroup/memory/memory.usage_in_bytes ] && [ -r /sys/fs/cgroup/memory/memory.limit_in_bytes ]; then used=$(cat /sys/fs/cgroup/memory/memory.usage_in_bytes); limit=$(cat /sys/fs/cgroup/memory/memory.limit_in_bytes); inactive=$(grep ''^total_inactive_file '' /sys/fs/cgroup/memory/memory.stat | cut -d '' '' -f2); if [ -z "$inactive" ]; then inactive=$(grep ''^inactive_file '' /sys/fs/cgroup/memory/memory.stat | cut -d '' '' -f2); fi; else exit 72; fi; if [ -z "$inactive" ]; then inactive=0; fi; printf "%s|%s|%s" "$used" "$limit" "$inactive"'
  $output = @(& $KubectlPath exec -n $Namespace "deployment/$DeploymentName" -c $ContainerName -- sh -ec $probe 2>&1)
  $exitCode = $LASTEXITCODE
  $memoryText = ($output -join "`n").Trim()
  if ($exitCode -ne 0) { throw "cgroup memory probe failed for ${DeploymentName}/${ContainerName}: $memoryText" }
  if ($memoryText -notmatch '^(?<used>\d+)\|(?<limit>\d+)\|(?<inactive>\d+)$') {
    throw "cgroup memory probe returned an invalid or unlimited value for ${DeploymentName}/${ContainerName}: $memoryText"
  }
  $usedBytes = [long]::Parse($matches.used, [Globalization.CultureInfo]::InvariantCulture)
  $limitBytes = [long]::Parse($matches.limit, [Globalization.CultureInfo]::InvariantCulture)
  $inactiveFileBytes = [long]::Parse($matches.inactive, [Globalization.CultureInfo]::InvariantCulture)
  if ($limitBytes -le 0 -or $limitBytes -ge 9000000000000000000) {
    throw "cgroup memory limit is not finite for ${DeploymentName}/${ContainerName}"
  }
  $inactiveFileBytes = [Math]::Min($usedBytes, $inactiveFileBytes)
  $effectiveUsedBytes = $usedBytes - $inactiveFileBytes
  $headroomGiB = [Math]::Round(($limitBytes - $effectiveUsedBytes) / 1GB, 3)
  return [ordered]@{
    deployment = $DeploymentName; container = $ContainerName
    raw_used_gib = [Math]::Round($usedBytes / 1GB, 3)
    reclaimable_inactive_file_gib = [Math]::Round($inactiveFileBytes / 1GB, 3)
    effective_used_gib = [Math]::Round($effectiveUsedBytes / 1GB, 3)
    limit_gib = [Math]::Round($limitBytes / 1GB, 3)
    raw_headroom_gib = [Math]::Round(($limitBytes - $usedBytes) / 1GB, 3)
    headroom_gib = $headroomGiB
  }
}

function Get-ResourceSafetySnapshot([string]$DockerPath, [string]$Profile, [string]$OutputPath) {
  $blockers = @()
  $hostMemory = [ordered]@{ free_gib = $null; total_gib = $null }
  $systemDisk = [ordered]@{ drive = $null; free_gib = $null }
  $outputDisk = [ordered]@{ drive = $null; free_gib = $null }
  $nodes = @()
  $workloads = @()
  $minimums = [ordered]@{ host_free_memory_gib = 8; system_drive_free_gib = 10; output_drive_free_gib = 20; minikube_node_headroom_gib = 0.5; mysql_container_headroom_gib = 0.1 }

  try {
    $os = Get-CimInstance -ClassName Win32_OperatingSystem -ErrorAction Stop
    $hostMemory.free_gib = [Math]::Round([double]$os.FreePhysicalMemory / 1MB, 2)
    $hostMemory.total_gib = [Math]::Round([double]$os.TotalVisibleMemorySize / 1MB, 2)
    if ($hostMemory.free_gib -lt $minimums.host_free_memory_gib) {
      $blockers += "host_free_memory_below_$($minimums.host_free_memory_gib)GiB"
    }
  } catch {
    $blockers += "host_memory_telemetry_unavailable: $($_.Exception.Message)"
  }

  try {
    $systemDriveLetter = ([string]$env:SystemDrive).TrimEnd('\').TrimEnd(':')
    if ([string]::IsNullOrWhiteSpace($systemDriveLetter)) { throw 'Windows system drive is unavailable' }
    $drive = Get-PSDrive -Name $systemDriveLetter -ErrorAction Stop
    $systemDisk.drive = $systemDriveLetter
    $systemDisk.free_gib = [Math]::Round([double]$drive.Free / 1GB, 2)
    if ($systemDisk.free_gib -lt $minimums.system_drive_free_gib) {
      $blockers += "system_drive_free_space_below_$($minimums.system_drive_free_gib)GiB"
    }
  } catch {
    $blockers += "system_drive_telemetry_unavailable: $($_.Exception.Message)"
  }

  try {
    $driveRoot = [System.IO.Path]::GetPathRoot($OutputPath)
    $driveLetter = $driveRoot.TrimEnd('\').TrimEnd(':')
    if ([string]::IsNullOrWhiteSpace($driveLetter)) { throw "cannot identify output drive from $OutputPath" }
    $drive = Get-PSDrive -Name $driveLetter -ErrorAction Stop
    $outputDisk.drive = $driveLetter
    $outputDisk.free_gib = [Math]::Round([double]$drive.Free / 1GB, 2)
    if ($outputDisk.free_gib -lt $minimums.output_drive_free_gib) {
      $blockers += "output_drive_free_space_below_$($minimums.output_drive_free_gib)GiB"
    }
  } catch {
    $blockers += "output_drive_telemetry_unavailable: $($_.Exception.Message)"
  }

  if ($RuntimeDatabasePlacement -eq 'isolated-kubernetes') {
    if ([string]::IsNullOrWhiteSpace($Profile)) {
      $blockers += 'isolated_kubernetes_requires_explicit_six_node_profile'
    } else {
      $kubectl = Get-ToolPath 'kubectl'
      if (-not $kubectl) {
        $blockers += 'kubectl_unavailable_for_six_node_resource_preflight'
      } else {
        try {
          $context = (& $kubectl config current-context 2>&1 | Out-String).Trim()
          if ($LASTEXITCODE -ne 0 -or $context -ne $Profile) {
            throw "current kubectl context '$context' does not match requested profile '$Profile'"
          }
          $nodesRaw = (& $kubectl get nodes -o json 2>&1 | Out-String)
          if ($LASTEXITCODE -ne 0) { throw "kubectl get nodes failed: $nodesRaw" }
          $nodeData = $nodesRaw | ConvertFrom-Json
          $expectedNames = @($Profile) + @(2..6 | ForEach-Object { '{0}-m{1:d2}' -f $Profile, $_ })
          $readyNames = @($nodeData.items | Where-Object {
            $readyCondition = $_.status.conditions | Where-Object { $_.type -eq 'Ready' } | Select-Object -Last 1
            $readyCondition.status -eq 'True'
          } | ForEach-Object { $_.metadata.name })
          $missingNodes = @($expectedNames | Where-Object { $_ -notin $readyNames })
          if ($missingNodes.Count -gt 0 -or $readyNames.Count -ne 6) {
            throw "expected six Ready nodes for '$Profile'; observed $($readyNames.Count), missing: $($missingNodes -join ',')"
          }

          if (-not $DockerPath) { throw 'Docker CLI unavailable for per-node memory headroom check' }
          $statsLines = @(& $DockerPath stats --no-stream --format '{{.Name}}|{{.MemUsage}}' 2>&1)
          if ($LASTEXITCODE -ne 0) { throw "Docker stats failed: $($statsLines -join ' ')" }
          foreach ($nodeName in $expectedNames) {
            $line = $statsLines | Where-Object { $_ -match ('^' + [Regex]::Escape($nodeName) + '\|') } | Select-Object -First 1
            if (-not $line) { throw "Docker stats did not report Minikube node container $nodeName" }
            $match = [Regex]::Match([string]$line, '\|\s*(?<used>[^/]+)\s*/\s*(?<limit>.+)$')
            if (-not $match.Success) { throw "could not parse Docker memory usage for ${nodeName}: $line" }
            $usedBytes = Convert-MemorySizeToBytes $match.Groups['used'].Value
            $limitBytes = Convert-MemorySizeToBytes $match.Groups['limit'].Value
            $headroomGiB = [Math]::Round(($limitBytes - $usedBytes) / 1GB, 3)
            $nodes += [ordered]@{ name = $nodeName; used_gib = [Math]::Round($usedBytes / 1GB, 3); limit_gib = [Math]::Round($limitBytes / 1GB, 3); headroom_gib = $headroomGiB }
            if ($headroomGiB -lt $minimums.minikube_node_headroom_gib) {
              $blockers += "${nodeName}_memory_headroom_below_$($minimums.minikube_node_headroom_gib)GiB"
            }
          }
          $mysqlMemory = Get-KubernetesContainerCgroupMemory $kubectl 'stablepay-e3-local' 'stablepay-e3-mysql' 'mysql'
          $workloads += $mysqlMemory
          if ($mysqlMemory.headroom_gib -lt $minimums.mysql_container_headroom_gib) {
            $blockers += "stablepay_e3_mysql_cgroup_headroom_below_$($minimums.mysql_container_headroom_gib)GiB"
          }
        } catch {
          $blockers += "six_node_resource_telemetry_unavailable: $($_.Exception.Message)"
        }
      }
    }
  }

  return [ordered]@{
    status = $(if ($blockers.Count -eq 0) { 'PASS' } else { 'BLOCKED' })
    captured_at = [DateTime]::UtcNow.ToString('o')
    minimums = $minimums
    host_memory = $hostMemory
    system_drive = $systemDisk
    output_drive = $outputDisk
    minikube_nodes = $nodes
    kubernetes_workloads = $workloads
    blockers = @($blockers)
  }
}

function Get-E3RunStatus([string]$StopReason, [object[]]$Rows) {
  switch ($StopReason) {
    'threshold_failure' { return 'STOPPED_EARLY_AFTER_THRESHOLD_FAILURE' }
    'process_failure' { return 'STOPPED_EARLY_AFTER_PROCESS_FAILURE' }
    'resource_guard_failure' { return 'STOPPED_EARLY_AFTER_RESOURCE_GUARD' }
  }
  if (@($Rows | Where-Object thresholds_failed).Count -gt 0) { return 'COMPLETE_WITH_THRESHOLD_FAILURES' }
  if (@($Rows | Where-Object process_failed).Count -gt 0) { return 'COMPLETE_WITH_PROCESS_FAILURES' }
  return 'COMPLETE'
}

function Write-NotRun([System.Collections.IDictionary]$Manifest, [string]$Reason) {
  $Manifest.status = 'NOT RUN'
  $Manifest.finished_at = [DateTime]::UtcNow.ToString('o')
  $Manifest.notes = @($Reason)
  $Manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
  @{ benchmark = 'E3'; status = 'NOT RUN'; reason = $Reason; resource_safety_checks = $Manifest.resource_safety_checks; evidence = @('manifest.json', 'metrics.json') } |
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
  runtime_variant = 'real-runtime-operator-attested-deterministic-local-dependencies'; workload_config_hash = "sha256:$configHash"
  k6_image = $K6Image; k6_image_tag = ''; k6_image_digest = ''; docker_network_mode = $DockerNetworkMode
  environment = [ordered]@{
    os = [System.Environment]::OSVersion.VersionString; cpu_count = [Environment]::ProcessorCount
    go_version = $goVersion; docker_version = $dockerVersion
    mysql = $(
      if ($RuntimeDatabasePlacement -eq 'isolated-kubernetes') { 'configured_in_isolated_kubernetes' }
      elseif ($RuntimeDatabasePlacement -eq 'host' -and $env:COMMERCE_RUNTIME_MYSQL_DSN) { 'configured_on_host' }
      elseif ($RuntimeDatabasePlacement -eq 'host') { 'not_configured_on_host' }
      else { 'placement_unknown' }
    )
    runtime_database_placement = $RuntimeDatabasePlacement
    kubernetes_profile = $(if ($KubernetesProfile) { $KubernetesProfile } else { 'not_applicable' })
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
  payment_side_effect_boundary = 'OPERATOR_CONFIRMED_DETERMINISTIC_LOCAL_DEPENDENCIES'
}
$manifest.environment.container_base_url = $containerBaseUrl
$baseNotes = @($manifest.notes | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) })

$resourceSnapshot = Get-ResourceSafetySnapshot $dockerPath $KubernetesProfile $OutputRoot
$manifest.environment.resource_preflight = $resourceSnapshot
$resourceSafetyChecks = @([ordered]@{ phase = 'before_matrix'; concurrency = $null; snapshot = $resourceSnapshot })
$manifest.resource_safety_checks = $resourceSafetyChecks
if ($resourceSnapshot.status -ne 'PASS') {
  $reason = "resource safety preflight blocked the load matrix: $($resourceSnapshot.blockers -join '; ')"
  Write-NotRun $manifest $reason
  exit 0
}

if ($K6Image -match '^(?<name>[^@]+?)(?::(?<tag>[^:]+))?$') { $manifest.k6_image_tag = $(if ($matches.tag) { $matches.tag } else { 'latest' }) }
if (-not $dockerPath) { Write-NotRun $manifest 'Docker CLI is unavailable; Docker Desktop must be installed and reachable'; exit 0 }

$manifest.k6_image_digest = Get-ImageDigest $dockerPath $K6Image
if ($PullK6Image) {
  try {
    & $dockerPath pull $K6Image | Tee-Object -FilePath (Join-Path $OutputRoot 'docker-pull.log')
    if ($LASTEXITCODE -ne 0) { throw "docker pull failed for $K6Image" }
    $manifest.k6_image_digest = Get-ImageDigest $dockerPath $K6Image
    if ([string]::IsNullOrWhiteSpace($manifest.k6_image_digest)) { throw "could not resolve RepoDigest for $K6Image" }
  } catch {
    $cachedDigest = Get-ImageDigest $dockerPath $K6Image
    if ([string]::IsNullOrWhiteSpace($cachedDigest)) {
      Write-NotRun $manifest $_.Exception.Message
      exit 0
    }
    $manifest.k6_image_digest = $cachedDigest
    $manifest.notes = @($baseNotes) + @("docker pull unavailable; using cached image $cachedDigest")
  }
} elseif ([string]::IsNullOrWhiteSpace($manifest.k6_image_digest)) {
  Write-NotRun $manifest "k6 image $K6Image is not present in the local Docker cache; no download attempted. Pass -PullK6Image only when you approve the image download."
  exit 0
} else {
  $manifest.notes = @($baseNotes) + @("using locally cached image $($manifest.k6_image_digest); no pull attempted")
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
$stoppedEarly = $false
$stoppedEarlyReason = $null
$concurrencyLevels = @($ConcurrencyMatrix -split ',' | ForEach-Object { [int]$_.Trim() })
foreach ($concurrency in $concurrencyLevels) {
  $dir = Join-Path $OutputRoot ("k6/concurrency-{0}" -f $concurrency)
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $summary = '/out/summary.json'
  $hostDir = (Resolve-Path -LiteralPath $dir).Path
  $hostScript = (Resolve-Path -LiteralPath $scriptPath).Path
  $dockerArgs = @(
    'run', '--rm', '--pull=never', '--network', $DockerNetworkMode,
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
  $processFailed = $k6ExitCode -ne 0
  $thresholdsFailed = $false
  $thresholdFailures = @()
  if ($processFailed) {
    $runFailures += "concurrency $concurrency exited with k6 code $k6ExitCode; inspect k6/concurrency-$concurrency/summary.json"
  }
  if (-not (Test-Path -LiteralPath $summaryPath -PathType Leaf)) {
    $runFailures += "concurrency $concurrency produced no summary.json"
    $processFailed = $true
  }
  $measurement = $null
  if (Test-Path -LiteralPath $summaryPath -PathType Leaf) {
    try {
      $summaryData = Get-Content -LiteralPath $summaryPath -Raw | ConvertFrom-Json
      if (-not $summaryData.metrics) { throw 'k6 summary is missing the metrics object' }
      $thresholdFailures = @(Get-K6ThresholdFailures $summaryData)
      $thresholdsFailed = $thresholdFailures.Count -gt 0
      if ($thresholdsFailed) {
        $runFailures += "concurrency $concurrency crossed k6 thresholds: $($thresholdFailures -join ', ')"
      }
      $measurementSeconds = ConvertTo-DurationSeconds $Duration
      $measuredIterations = Get-SummaryMetricValue $summaryData 'measurement_iterations' 'count'
      $measuredHTTPRequests = Get-SummaryMetricValue $summaryData 'measurement_http_requests' 'count'
      $measuredCompletedEpisodes = Get-SummaryMetricValue $summaryData 'measurement_completed_episodes' 'count'
      $measuredCreateStatuses = [ordered]@{}
      $measuredPollStatuses = [ordered]@{}
      foreach ($statusCode in @(200, 201, 202, 400, 401, 404, 409, 422, 500, 502, 503, 504)) {
        $measuredCreateStatuses[[string]$statusCode] = Get-SummaryMetricValue $summaryData "measurement_create_status_$statusCode" 'count' 0
        $measuredPollStatuses[[string]$statusCode] = Get-SummaryMetricValue $summaryData "measurement_status_poll_status_$statusCode" 'count' 0
      }
      $measuredCreateStatuses.other = Get-SummaryMetricValue $summaryData 'measurement_create_status_other' 'count' 0
      $measuredPollStatuses.other = Get-SummaryMetricValue $summaryData 'measurement_status_poll_status_other' 'count' 0
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
      $processFailed = $true
    }
  }
  $rows += [ordered]@{
    concurrency = $concurrency; warmup = $Warmup; duration = $Duration
    exit_code = $k6ExitCode; process_failed = $processFailed
    thresholds_failed = $thresholdsFailed; threshold_failures = $thresholdFailures
    measurement = $measurement; summary = "k6/concurrency-$concurrency/summary.json"
  }
  $manifest.status = 'RUNNING'
  $manifest.matrix = $rows
  $manifest.notes = @($baseNotes) + @($runFailures)
  $manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
  if ($StopOnThresholdFailure -and ($thresholdsFailed -or $processFailed)) {
    $stoppedEarly = $true
    if ($thresholdsFailed) {
      $stoppedEarlyReason = 'threshold_failure'
      $runFailures += "stopped remaining concurrency tiers after threshold failure at concurrency $concurrency"
    } else {
      $stoppedEarlyReason = 'process_failure'
      $runFailures += "stopped remaining concurrency tiers after process/evidence failure at concurrency $concurrency"
    }
    break
  }

  $postTierSnapshot = Get-ResourceSafetySnapshot $dockerPath $KubernetesProfile $OutputRoot
  $resourceSafetyChecks += [ordered]@{ phase = 'after_tier'; concurrency = $concurrency; snapshot = $postTierSnapshot }
  $manifest.resource_safety_checks = $resourceSafetyChecks
  if ($postTierSnapshot.status -ne 'PASS') {
    $stoppedEarly = $true
    $stoppedEarlyReason = 'resource_guard_failure'
    $runFailures += "stopped remaining concurrency tiers after the resource guard failed following concurrency ${concurrency}: $($postTierSnapshot.blockers -join '; ')"
    $manifest.status = 'RUNNING'
    $manifest.matrix = $rows
    $manifest.notes = @($baseNotes) + @($runFailures)
    $manifest | ConvertTo-Json -Depth 16 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
    break
  }
}

$manifest.status = Get-E3RunStatus $stoppedEarlyReason $rows
$manifest.finished_at = [DateTime]::UtcNow.ToString('o')
$manifest.environment.concurrency = $ConcurrencyMatrix
$manifest.environment.container_base_url = $containerBaseUrl
$manifest.requested_concurrency_matrix = $ConcurrencyMatrix
$manifest.completed_concurrency_tiers = @($rows | ForEach-Object { $_.concurrency })
$manifest.stopped_early = $stoppedEarly
$manifest.stopped_early_reason = $(if ($stoppedEarlyReason) { $stoppedEarlyReason } else { 'not_applicable' })
$manifest.notes = @($baseNotes) + @($runFailures)
$manifest | ConvertTo-Json -Depth 12 | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'manifest.json')
$metrics = [ordered]@{
  benchmark = 'E3'; status = $manifest.status; runtime_variant = $manifest.runtime_variant
  http_api_throughput = 'see k6 summaries'; business_episode_completion_throughput = 'see completed_episodes metric'
  matrix = $rows; resource_safety_checks = $resourceSafetyChecks
  docker = [ordered]@{ image = $K6Image; tag = $manifest.k6_image_tag; digest = $manifest.k6_image_digest; network_mode = $DockerNetworkMode }
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
  'Threshold status comes from the k6 summary flags; a non-zero process exit is recorded separately. With `-StopOnThresholdFailure`, either an actual threshold failure or a runner/evidence failure stops later tiers. Resource headroom is rechecked after every tier and independently stops higher tiers when the guard fails.', '',
  'E3A uses the real Runtime HTTP boundary with deterministic local dependencies. Duplicate settlement still requires a post-run ledger audit.'
)
$reportLines | Set-Content -Encoding UTF8 (Join-Path $OutputRoot 'report.md')
