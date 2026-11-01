param(
  [ValidateSet('payathon','campus')][string]$Competition = 'payathon',
  [ValidateSet('local','alipay_sandbox')][string]$PaymentMode = 'local',
  [string]$AlipayPublicOrigin = 'https://bubblevan.de5.net',
  [switch]$UseLLM,
  [switch]$SkipBuild
)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$runRoot = Join-Path $repoRoot '.local-run/procurement-mvp'
$privateRoot = Join-Path $repoRoot '.local-run/procurement-deploy/private'
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
$manifestPath = Join-Path $runRoot 'processes.json'
if (Test-Path -LiteralPath $manifestPath) {
  $previous = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
  foreach ($entry in $previous.processes) {
    if (Get-Process -Id $entry.pid -ErrorAction SilentlyContinue) { throw 'MVP process already exists. Run stop-procurement-mvp.ps1 first.' }
  }
}
foreach ($port in @(8093,28080)) {
  if (Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue) { throw "Port $port is already occupied." }
}

$composeEnv = Join-Path $privateRoot 'compose.env'
if (-not (Test-Path -LiteralPath $composeEnv)) {
  if (-not (Test-Path -LiteralPath (Join-Path $repoRoot '.local-run/secrets/alipay/app-private.pem'))) { throw 'Alipay sandbox keys are missing; prepare them before starting the sandbox stack.' }
  Push-Location $repoRoot
  try { & node scripts/prepare-alipay-sandbox.mjs $AlipayPublicOrigin; if ($LASTEXITCODE -ne 0) { throw 'Sandbox configuration preparation failed.' } } finally { Pop-Location }
}
foreach ($line in Get-Content -LiteralPath $composeEnv) {
  if ($line -match '^([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
    [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
  }
}
$env:FIAT_PAYMENT_MODE = $PaymentMode
$env:STABLEPAY_API_KEY = $env:GATEWAY_API_KEY
$env:STABLEPAY_GATEWAY_BASE_URL = 'http://127.0.0.1:28080'
$env:FIAT_SCHEMA_AUTO_MIGRATE = 'true'
$env:COMMERCE_RUNTIME_ALIPAY_ENABLED = $(if ($PaymentMode -eq 'alipay_sandbox') { '1' } else { '0' })

$safeRepoRoot = $repoRoot.Replace('\','/')
$composePrefix = @('-p','root','--env-file',$composeEnv,'-f',(Join-Path $repoRoot 'stablepayai-idl/docker-compose.infra.yml'),'-f',(Join-Path $repoRoot 'stablepayai-idl/docker-compose.services.yml'))
$profile = @()
if ($PaymentMode -eq 'alipay_sandbox') { $profile = @('--profile','alipay-sandbox') }
& docker compose @composePrefix @profile up -d --build
if ($LASTEXITCODE -ne 0) { throw 'StablePay microservice stack did not start. Check Docker Desktop and compose logs.' }

$runtimeBinary = Join-Path $runRoot 'procurement-runtime.exe'
$frontendDir = Join-Path $runRoot 'web'
if (-not $SkipBuild) {
  Push-Location (Join-Path $repoRoot 'commerce-runtime')
  try { & go build -buildvcs=false -o $runtimeBinary ./cmd/procurement-mvp; if ($LASTEXITCODE -ne 0) { throw 'Commerce Runtime build failed.' } } finally { Pop-Location }
  Push-Location (Join-Path $repoRoot 'stablepay-frontend')
  try {
    if (-not (Test-Path -LiteralPath 'node_modules')) { & npm ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed.' } }
    $buildScript = $(if ($Competition -eq 'payathon') { 'build:payathon' } else { 'build' })
    & npm run $buildScript -- --outDir $frontendDir; if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
  } finally { Pop-Location }
}
if (-not (Test-Path -LiteralPath $runtimeBinary)) { throw "Missing build: $runtimeBinary" }
function Wait-URL([string]$Url) {
  for ($attempt=0; $attempt -lt 90; $attempt++) {
    try { $null = Invoke-WebRequest -Uri $Url -TimeoutSec 2; return } catch { Start-Sleep -Milliseconds 500 }
  }
  throw "Service did not become ready: $Url. Inspect Docker and $runRoot logs."
}
Wait-URL 'http://127.0.0.1:28080/readyz'
$env:MVP_HTTP_ADDR = '127.0.0.1:8093'
$env:MVP_PUBLIC_ORIGIN = 'http://127.0.0.1:8093'
$env:MVP_PROFILE = $Competition
$env:MVP_USE_LLM = $(if ($UseLLM) { '1' } else { '0' })
$env:MVP_STORE_PATH = Join-Path $runRoot "runtime-$PaymentMode.json"
$env:MVP_STATIC_DIR = $frontendDir
$runtimeProcess = Start-Process -FilePath $runtimeBinary -WorkingDirectory (Join-Path $repoRoot 'commerce-runtime') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runRoot 'runtime.stdout.log') -RedirectStandardError (Join-Path $runRoot 'runtime.stderr.log')
try {
  Wait-URL 'http://127.0.0.1:8093/healthz'
  @{profile=$Competition; mode=$PaymentMode; processes=@(@{pid=$runtimeProcess.Id; path=$runtimeBinary})} | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $manifestPath -Encoding utf8
  Write-Output "MVP ready: http://127.0.0.1:8093/ ($Competition, $PaymentMode). Payment traffic uses Hertz Gateway → Payment Service Kitex."
} catch {
  Stop-Process -Id $runtimeProcess.Id -ErrorAction SilentlyContinue
  throw
}
