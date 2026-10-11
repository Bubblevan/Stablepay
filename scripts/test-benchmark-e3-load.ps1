$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$runnerPath = Join-Path $PSScriptRoot 'benchmark-e3-load.ps1'
$tokens = $null
$parseErrors = $null
$runnerAst = [System.Management.Automation.Language.Parser]::ParseFile(
  $runnerPath, [ref]$tokens, [ref]$parseErrors
)
if ($parseErrors.Count -gt 0) {
  throw "E3 runner has PowerShell parse errors: $($parseErrors -join '; ')"
}

foreach ($functionName in @('Get-SummaryMetricValue', 'Get-K6ThresholdFailures', 'Convert-MemorySizeToBytes', 'Get-KubernetesContainerCgroupMemory', 'Get-ResourceSafetySnapshot', 'Get-E3RunStatus')) {
  $functionAst = $runnerAst.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
      $node.Name -eq $functionName
  }, $true)
  if (-not $functionAst) { throw "E3 runner helper is missing: $functionName" }
  Invoke-Expression $functionAst.Extent.Text
}

function Assert-Equal([object]$Actual, [object]$Expected, [string]$Name) {
  if ($Actual -ne $Expected) {
    throw "$Name expected '$Expected', observed '$Actual'"
  }
}

$script:MockLowNodeMemory = $false
$script:MockLowMysqlMemory = $false
$script:MockReclaimableMysqlCache = $false
$script:MockLowSystemDisk = $false
function Get-CimInstance {
  [pscustomobject]@{ FreePhysicalMemory = 16 * 1MB; TotalVisibleMemorySize = 63 * 1MB }
}
function Get-PSDrive([string]$Name) {
  $free = if ($script:MockLowSystemDisk -and $Name -eq $env:SystemDrive.TrimEnd(':')) { 5 * 1GB } else { 100 * 1GB }
  [pscustomobject]@{ Name = $Name; Free = $free }
}
function Get-ToolPath([string]$Name) { return $Name }
function kubectl {
  if ($args[0] -eq 'config') {
    $global:LASTEXITCODE = 0
    return 'stablepay-six'
  }
  if ($args[0] -eq 'get' -and $args[1] -eq 'nodes') {
    $global:LASTEXITCODE = 0
    $names = @('stablepay-six', 'stablepay-six-m02', 'stablepay-six-m03', 'stablepay-six-m04', 'stablepay-six-m05', 'stablepay-six-m06')
    $items = foreach ($name in $names) {
      [pscustomobject]@{ metadata = [pscustomobject]@{ name = $name }; status = [pscustomobject]@{ conditions = @([pscustomobject]@{ type = 'Ready'; status = 'True' }) } }
    }
    return ([pscustomobject]@{ items = @($items) } | ConvertTo-Json -Depth 8 -Compress)
  }
  if ($args[0] -eq 'exec' -and $args[3] -eq 'deployment/stablepay-e3-mysql') {
    $global:LASTEXITCODE = 0
    if ($script:MockLowMysqlMemory) { return '1020054733|1073741824|0' }
    if ($script:MockReclaimableMysqlCache) { return '1073741824|1073741824|413278208' }
    return '805306368|1073741824|0'
  }
  throw "unexpected mocked kubectl call: $args"
}
function docker {
  if ($args[0] -ne 'stats') { throw "unexpected mocked docker call: $args" }
  $global:LASTEXITCODE = 0
  $names = @('stablepay-six', 'stablepay-six-m02', 'stablepay-six-m03', 'stablepay-six-m04', 'stablepay-six-m05', 'stablepay-six-m06')
  foreach ($name in $names) {
    $used = if ($script:MockLowNodeMemory -and $name -eq 'stablepay-six-m03') { '1.75GiB' } else { '1GiB' }
    "$name|$used / 2GiB"
  }
}

$passing = '{"metrics":{"http_req_failed{phase:measurement}":{"thresholds":{"rate<0.05":false},"value":0},"measurement_iterations":{"count":7}}}' | ConvertFrom-Json
$failing = '{"metrics":{"measurement_iterations":{"thresholds":{"count>0":true},"count":7}}}' | ConvertFrom-Json
$malformed = '{"metrics":{"measurement_iterations":{"thresholds":{"count>0":"false"}}}}' | ConvertFrom-Json

Assert-Equal (@(Get-K6ThresholdFailures $passing).Count) 0 'passing summary threshold count'
Assert-Equal (@(Get-K6ThresholdFailures $failing).Count) 1 'failed summary threshold count'
Assert-Equal (Get-E3RunStatus '' @()) 'COMPLETE' 'completed run status'
Assert-Equal (Get-E3RunStatus 'threshold_failure' @()) 'STOPPED_EARLY_AFTER_THRESHOLD_FAILURE' 'threshold stop status'
Assert-Equal (Get-E3RunStatus 'resource_guard_failure' @()) 'STOPPED_EARLY_AFTER_RESOURCE_GUARD' 'resource guard stop status'
Assert-Equal (Get-SummaryMetricValue $passing 'measurement_iterations' 'count') 7 'present summary metric'
Assert-Equal (Get-SummaryMetricValue $passing 'missing_optional_counter' 'count' 0) 0 'explicit optional counter default'
Assert-Equal (Convert-MemorySizeToBytes '1.5GiB') 1610612736 'Docker binary memory size'
Assert-Equal (Convert-MemorySizeToBytes '512MiB') 536870912 'Docker mebibyte memory size'
$mysqlCacheHeavyMemory = Get-KubernetesContainerCgroupMemory 'kubectl' 'stablepay-e3-local' 'stablepay-e3-mysql' 'mysql'
Assert-Equal $mysqlCacheHeavyMemory.headroom_gib 0.25 'MySQL container cgroup effective headroom'
$script:MockReclaimableMysqlCache = $true
$mysqlReclaimableCacheMemory = Get-KubernetesContainerCgroupMemory 'kubectl' 'stablepay-e3-local' 'stablepay-e3-mysql' 'mysql'
Assert-Equal $mysqlReclaimableCacheMemory.raw_headroom_gib 0 'MySQL raw cgroup headroom with full file cache'
Assert-Equal $mysqlReclaimableCacheMemory.headroom_gib 0.385 'MySQL effective headroom after reclaimable cache'
$script:MockReclaimableMysqlCache = $false
$script:MockLowMysqlMemory = $true
$mysqlReclaimableCacheMemory = Get-KubernetesContainerCgroupMemory 'kubectl' 'stablepay-e3-local' 'stablepay-e3-mysql' 'mysql'
Assert-Equal $mysqlReclaimableCacheMemory.raw_headroom_gib 0.05 'MySQL raw cgroup headroom'
Assert-Equal $mysqlReclaimableCacheMemory.headroom_gib 0.05 'MySQL cgroup headroom without reclaimable cache'
$script:MockLowMysqlMemory = $false
$script:RuntimeDatabasePlacement = 'isolated-kubernetes'
$safeSnapshot = Get-ResourceSafetySnapshot 'docker' 'stablepay-six' 'D:\bench\run'
Assert-Equal $safeSnapshot.status 'PASS' 'resource preflight safe snapshot'
Assert-Equal $safeSnapshot.minikube_nodes.Count 6 'resource preflight six node count'
Assert-Equal $safeSnapshot.kubernetes_workloads.Count 1 'resource preflight workload cgroup count'
$script:MockLowNodeMemory = $true
$constrainedSnapshot = Get-ResourceSafetySnapshot 'docker' 'stablepay-six' 'D:\bench\run'
Assert-Equal $constrainedSnapshot.status 'BLOCKED' 'resource preflight constrained snapshot'
Assert-Equal (@($constrainedSnapshot.blockers | Where-Object { $_ -like 'stablepay-six-m03_memory_headroom*' }).Count) 1 'resource preflight low-node blocker'
$script:MockLowNodeMemory = $false
$script:MockLowMysqlMemory = $true
$lowMysqlMemorySnapshot = Get-ResourceSafetySnapshot 'docker' 'stablepay-six' 'D:\bench\run'
Assert-Equal $lowMysqlMemorySnapshot.status 'BLOCKED' 'resource preflight constrained MySQL cgroup snapshot'
Assert-Equal (@($lowMysqlMemorySnapshot.blockers | Where-Object { $_ -eq 'stablepay_e3_mysql_cgroup_headroom_below_0.1GiB' }).Count) 1 'resource preflight low-MySQL-cgroup blocker'
$script:MockLowMysqlMemory = $false
$script:MockLowSystemDisk = $true
$lowDiskSnapshot = Get-ResourceSafetySnapshot 'docker' 'stablepay-six' 'D:\bench\run'
Assert-Equal $lowDiskSnapshot.status 'BLOCKED' 'resource preflight low-system-disk snapshot'
Assert-Equal (@($lowDiskSnapshot.blockers | Where-Object { $_ -eq 'system_drive_free_space_below_10GiB' }).Count) 1 'resource preflight low-system-disk blocker'
$script:MockLowSystemDisk = $false

$missingRequiredRejected = $false
try { Get-SummaryMetricValue $passing 'missing_required_metric' 'count' | Out-Null }
catch { $missingRequiredRejected = $true }
Assert-Equal $missingRequiredRejected $true 'missing required metric rejection'

$malformedThresholdRejected = $false
try { Get-K6ThresholdFailures $malformed | Out-Null }
catch { $malformedThresholdRejected = $true }
Assert-Equal $malformedThresholdRejected $true 'malformed threshold flag rejection'

$malformedMemoryRejected = $false
try { Convert-MemorySizeToBytes 'unknown' | Out-Null }
catch { $malformedMemoryRejected = $true }
Assert-Equal $malformedMemoryRejected $true 'malformed Docker memory size rejection'

[pscustomobject]@{ status = 'PASS'; checks = 24; runner = (Split-Path -Leaf $runnerPath) } |
  ConvertTo-Json -Compress
