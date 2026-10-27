param(
  [ValidateSet('payathon','campus')][string]$Competition = 'payathon',
  [ValidateSet('local','alipay_sandbox')][string]$PaymentMode = 'local',
  [switch]$UseLLM,
  [switch]$SkipBuild
)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$runRoot = Join-Path $repoRoot '.local-run/procurement-mvp'
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
$manifestPath = Join-Path $runRoot 'processes.json'
if (Test-Path -LiteralPath $manifestPath) {
  $previous = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
  foreach ($entry in $previous.processes) {
    if (Get-Process -Id $entry.pid -ErrorAction SilentlyContinue) { throw 'MVP processes already exist. Run stop-procurement-mvp.ps1 first.' }
  }
}
foreach ($port in @(8092,8093)) {
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
$env:MVP_STORE_PATH = Join-Path $runRoot "runtime-$PaymentMode.json"
$env:MVP_STATIC_DIR = Join-Path $runRoot 'web'
$env:GOCACHE = Join-Path (Split-Path $repoRoot -Parent) '.go-build-cache'
$env:GOMODCACHE = Join-Path (Split-Path $repoRoot -Parent) '.go-mod-cache'
$paymentBinary = Join-Path $runRoot 'payment-fiat.exe'
$runtimeBinary = Join-Path $runRoot 'procurement-runtime.exe'
if (-not $SkipBuild) {
  Push-Location (Join-Path $repoRoot 'payment-service')
  try { & go build -buildvcs=false -o $paymentBinary ./cmd/fiat-server; if ($LASTEXITCODE -ne 0) { throw 'payment-service build failed' } } finally { Pop-Location }
  Push-Location (Join-Path $repoRoot 'commerce-runtime')
  try { & go build -buildvcs=false -o $runtimeBinary ./cmd/procurement-mvp; if ($LASTEXITCODE -ne 0) { throw 'commerce-runtime build failed' } } finally { Pop-Location }
  Push-Location (Join-Path $repoRoot 'stablepay-frontend')
  try {
    if (-not (Test-Path -LiteralPath 'node_modules')) { & npm ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } }
    $buildScript = $(if ($Competition -eq 'payathon') { 'build:payathon' } else { 'build' })
    & npm run $buildScript -- --outDir $env:MVP_STATIC_DIR; if ($LASTEXITCODE -ne 0) { throw 'frontend build failed' }
  } finally { Pop-Location }
}
foreach ($binary in @($paymentBinary,$runtimeBinary)) { if (-not (Test-Path -LiteralPath $binary)) { throw "Missing build: $binary" } }
function Wait-MVP([string]$Url) {
  for ($attempt=0; $attempt -lt 60; $attempt++) {
    try { $null = Invoke-RestMethod -Uri $Url -TimeoutSec 1; return } catch { Start-Sleep -Milliseconds 250 }
  }
  throw "Service did not become ready: $Url. Inspect logs in $runRoot"
}
$started = @()
try {
  $paymentProcess = Start-Process -FilePath $paymentBinary -WorkingDirectory (Join-Path $repoRoot 'payment-service') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runRoot 'payment.stdout.log') -RedirectStandardError (Join-Path $runRoot 'payment.stderr.log')
  $started += @{pid=$paymentProcess.Id; path=$paymentBinary}
  Wait-MVP 'http://127.0.0.1:8092/healthz'
  $runtimeProcess = Start-Process -FilePath $runtimeBinary -WorkingDirectory (Join-Path $repoRoot 'commerce-runtime') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runRoot 'runtime.stdout.log') -RedirectStandardError (Join-Path $runRoot 'runtime.stderr.log')
  $started += @{pid=$runtimeProcess.Id; path=$runtimeBinary}
  Wait-MVP 'http://127.0.0.1:8093/api/mvp/status'
  @{profile=$Competition; mode=$PaymentMode; processes=$started} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $manifestPath -Encoding utf8
  Write-Output "MVP ready: http://127.0.0.1:8093/ ($Competition, $PaymentMode)"
} catch {
  foreach ($entry in $started) { Stop-Process -Id $entry.pid -ErrorAction SilentlyContinue }
  throw
}
