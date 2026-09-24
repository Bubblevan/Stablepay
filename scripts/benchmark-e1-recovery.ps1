param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [int64]$Seed = 42,
  [int]$DeepSeekTrials = 3,
  [switch]$RunDeepSeek
)
$ErrorActionPreference = 'Stop'

function Import-BenchmarkDotEnv([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw 'E1 DeepSeek run requires the repository .env file.'
  }

  $allowedNames = @(
    'LLM_PROVIDER', 'LLM_BASE_URL', 'LLM_API_KEY', 'LLM_MODEL', 'LLM_MODEL_ID',
    'DEEPSEEK_INPUT_PRICE_PER_MILLION_USD', 'DEEPSEEK_OUTPUT_PRICE_PER_MILLION_USD'
  )
  $importedNames = [System.Collections.Generic.List[string]]::new()
  try {
    foreach ($line in [System.IO.File]::ReadLines($Path)) {
      if ($line -match '^\s*(#|$)') { continue }
      if ($line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') { continue }

      $name = $Matches[1]
      if ($name -notin $allowedNames) { continue }
      if (-not [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name, 'Process'))) { continue }

      $value = $Matches[2]
      if ($value.Length -ge 2 -and (($value[0] -eq '"' -and $value[$value.Length - 1] -eq '"') -or ($value[0] -eq "'" -and $value[$value.Length - 1] -eq "'"))) {
        $value = $value.Substring(1, $value.Length - 2)
      }
      $importedNames.Add($name)
      [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }

    $model = [Environment]::GetEnvironmentVariable('LLM_MODEL', 'Process')
    if ([string]::IsNullOrWhiteSpace($model)) { $model = [Environment]::GetEnvironmentVariable('LLM_MODEL_ID', 'Process') }
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable('LLM_BASE_URL', 'Process'))) {
      throw 'E1 DeepSeek run is missing LLM_BASE_URL.'
    }
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable('LLM_API_KEY', 'Process'))) {
      throw 'E1 DeepSeek run is missing LLM_API_KEY.'
    }
    if ([string]::IsNullOrWhiteSpace($model)) { throw 'E1 DeepSeek run is missing LLM_MODEL or LLM_MODEL_ID.' }
  } catch {
    foreach ($name in $importedNames) { [Environment]::SetEnvironmentVariable($name, $null, 'Process') }
    throw
  }
  return ,$importedNames
}

$importedNames = @()
if ($RunDeepSeek) { $importedNames = Import-BenchmarkDotEnv (Join-Path $RepoRoot '.env') }
$argsList = @('run', './cmd/resume-benchmark', '-benchmark=e1', "-repo-root=$RepoRoot", "-seed=$Seed", "-deepseek-trials=$DeepSeekTrials")
if ($RunDeepSeek) { $argsList += '-run-deepseek' }
$locationPushed = $false
$exitCode = 1
try {
  Push-Location (Join-Path $RepoRoot 'commerce-runtime')
  $locationPushed = $true
  & go @argsList
  $exitCode = $LASTEXITCODE
} finally {
  if ($locationPushed) { Pop-Location }
  foreach ($name in $importedNames) { [Environment]::SetEnvironmentVariable($name, $null, 'Process') }
}
if ($exitCode -ne 0) { exit $exitCode }
