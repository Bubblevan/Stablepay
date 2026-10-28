param(
  [ValidateSet('payathon','campus')][string]$Competition = 'payathon',
  [ValidateSet('local','alipay_sandbox')][string]$PaymentMode = 'local',
  [switch]$UseLLM,
  [switch]$SkipBuild
)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if ($UseLLM) {
  $allowedModelNames = @('LLM_PROVIDER','LLM_BASE_URL','LLM_API_KEY','LLM_MODEL','LLM_MODEL_ID')
  $modelEnvPath = Join-Path $repoRoot '.env'
  if (Test-Path -LiteralPath $modelEnvPath) {
    foreach ($line in [IO.File]::ReadLines($modelEnvPath)) {
      if ($line -match '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$') {
        $name = $Matches[1]
        $value = $Matches[2]
        if ($name -notin $allowedModelNames -or -not [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name,'Process'))) { continue }
        if ($value -match '^["'']') {
          $quote = $value.Substring(0,1)
          $end = $value.IndexOf($quote,1)
          if ($end -lt 1 -or $value.Substring($end+1) -notmatch '^\s*(?:#.*)?$') { throw "Invalid quoting for $name in .env" }
          $value = $value.Substring(1,$end-1)
        } else { $value = ($value -replace '\s+#.*$','').Trim() }
        [Environment]::SetEnvironmentVariable($name,$value,'Process')
      }
    }
  }
  if ([string]::IsNullOrWhiteSpace($env:LLM_PROVIDER)) { $env:LLM_PROVIDER = 'openai-compatible' }
  if ([string]::IsNullOrWhiteSpace($env:LLM_MODEL)) { $env:LLM_MODEL = $env:LLM_MODEL_ID }
  foreach ($name in @('LLM_BASE_URL','LLM_API_KEY','LLM_MODEL')) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name,'Process'))) { throw "Missing model configuration: $name" }
  }
}
$runRoot = Join-Path $repoRoot '.local-run/procurement-mvp'
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
$manifestPath = Join-Path $runRoot 'processes.json'
if (Test-Path -LiteralPath $manifestPath) {
  $previous = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
  foreach ($entry in $previous.processes) {
    if (Get-Process -Id $entry.pid -ErrorAction SilentlyContinue) { throw 'MVP processes already exist. Run stop-procurement-mvp.ps1 first.' }
  }
}
$ports = $(if ($Competition -eq 'campus') { @(8093) } else { @(8092,8093) })
foreach ($port in $ports) {
  if (Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue) { throw "Port $port is already occupied." }
}
if ([string]::IsNullOrWhiteSpace($env:FIAT_INTERNAL_TOKEN)) {
  $tokenBytes = New-Object byte[] 32
  [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($tokenBytes)
  $env:FIAT_INTERNAL_TOKEN = -join ($tokenBytes | ForEach-Object { $_.ToString('x2') })
}
$env:FIAT_PAYMENT_MODE = $PaymentMode
$env:FIAT_HTTP_ADDR = '127.0.0.1:8092'
$env:FIAT_PAYMENT_URL = 'http://127.0.0.1:8092'
$env:MVP_HTTP_ADDR = '127.0.0.1:8093'
$env:MVP_PUBLIC_ORIGIN = 'http://127.0.0.1:8093'
$env:MVP_PROFILE = $Competition
$env:MVP_USE_LLM = $(if ($UseLLM) { '1' } else { '0' })
$env:FIAT_STORE_PATH = Join-Path $runRoot "fiat-$PaymentMode.json"
$env:MVP_STORE_PATH = Join-Path $runRoot "runtime-$Competition-$PaymentMode.json"
$env:MVP_STATIC_DIR = Join-Path $runRoot 'web'
$env:GOCACHE = Join-Path (Split-Path $repoRoot -Parent) '.go-build-cache'
$env:GOMODCACHE = Join-Path (Split-Path $repoRoot -Parent) '.go-mod-cache'
$paymentBinary = Join-Path $runRoot 'payment-fiat.exe'
$runtimeBinary = Join-Path $runRoot 'procurement-runtime.exe'
if (-not $SkipBuild) {
  if ($Competition -ne 'campus') {
    Push-Location (Join-Path $repoRoot 'payment-service')
    try { & go build -buildvcs=false -o $paymentBinary ./cmd/fiat-server; if ($LASTEXITCODE -ne 0) { throw 'payment-service build failed' } } finally { Pop-Location }
  }
  Push-Location (Join-Path $repoRoot 'commerce-runtime')
  try { & go build -buildvcs=false -o $runtimeBinary ./cmd/procurement-mvp; if ($LASTEXITCODE -ne 0) { throw 'commerce-runtime build failed' } } finally { Pop-Location }
  Push-Location (Join-Path $repoRoot 'stablepay-frontend')
  try {
    if (-not (Test-Path -LiteralPath 'node_modules')) { & npm ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } }
    $buildScript = $(if ($Competition -eq 'payathon') { 'build:payathon' } else { 'build' })
    & npm run $buildScript -- --outDir $env:MVP_STATIC_DIR; if ($LASTEXITCODE -ne 0) { throw 'frontend build failed' }
  } finally { Pop-Location }
}
$binaries = $(if ($Competition -eq 'campus') { @($runtimeBinary) } else { @($paymentBinary,$runtimeBinary) })
foreach ($binary in $binaries) { if (-not (Test-Path -LiteralPath $binary)) { throw "Missing build: $binary" } }
function Wait-MVP([string]$Url) {
  for ($attempt=0; $attempt -lt 60; $attempt++) {
    try { $null = Invoke-RestMethod -Uri $Url -TimeoutSec 1; return } catch { Start-Sleep -Milliseconds 250 }
  }
  throw "Service did not become ready: $Url. Inspect logs in $runRoot"
}
$started = @()
try {
  if ($Competition -ne 'campus') {
    $paymentProcess = Start-Process -FilePath $paymentBinary -WorkingDirectory (Join-Path $repoRoot 'payment-service') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runRoot 'payment.stdout.log') -RedirectStandardError (Join-Path $runRoot 'payment.stderr.log')
    $started += @{pid=$paymentProcess.Id; path=$paymentBinary}
    Wait-MVP 'http://127.0.0.1:8092/healthz'
  }
  $runtimeProcess = Start-Process -FilePath $runtimeBinary -WorkingDirectory (Join-Path $repoRoot 'commerce-runtime') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runRoot 'runtime.stdout.log') -RedirectStandardError (Join-Path $runRoot 'runtime.stderr.log')
  $started += @{pid=$runtimeProcess.Id; path=$runtimeBinary}
  Wait-MVP 'http://127.0.0.1:8093/api/mvp/status'
  @{profile=$Competition; mode=$PaymentMode; processes=$started} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $manifestPath -Encoding utf8
  Write-Output "MVP ready: http://127.0.0.1:8093/ ($Competition, $PaymentMode)"
} catch {
  foreach ($entry in $started) { Stop-Process -Id $entry.pid -ErrorAction SilentlyContinue }
  throw
}
