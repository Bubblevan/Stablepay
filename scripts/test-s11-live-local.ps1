$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $repo 'commerce-runtime'
$outDir = Join-Path $repo '.local-run\s11-live-local'
$serverExe = Join-Path $outDir 's11-live-local.exe'
$evalExe = Join-Path $outDir 'stablepay-agent-eval.exe'
$cliExe = Join-Path $outDir 'stablepay-runtime.exe'
$address = '127.0.0.1:18090'
$baseUrl = 'http://127.0.0.1:18090'

New-Item -ItemType Directory -Force -Path $outDir | Out-Null
Push-Location $runtimeDir
$serverProcess = $null
try {
    go build -o $serverExe ./cmd/s11-live-local
    go build -o $evalExe ./cmd/stablepay-agent-eval
    go build -o $cliExe ./cmd/stablepay-runtime
    $serverProcess = Start-Process -FilePath $serverExe -ArgumentList @('--addr', $address, '--memory', 'on', '--llm', 'local') -WindowStyle Hidden -PassThru
    $ready = $false
    for ($attempt = 0; $attempt -lt 100; $attempt++) {
        try {
            $health = Invoke-WebRequest -UseBasicParsing -Uri ($baseUrl + '/healthz') -TimeoutSec 2
            if ($health.StatusCode -eq 200) { $ready = $true; break }
        } catch { }
        Start-Sleep -Milliseconds 100
    }
    if (-not $ready) { throw 'live-local runtime did not become healthy' }
    & $evalExe run --dataset (Join-Path $runtimeDir 'testdata\s11\live-local\scenarios.jsonl') --out-dir $outDir --server $baseUrl --injector $baseUrl --runtime-cli $cliExe --trials 8 --poll 50ms --timeout 45s
    if ($LASTEXITCODE -ne 0) { throw 'stablepay-agent-eval live-local run failed' }
    & $evalExe check --server $baseUrl
    if ($LASTEXITCODE -ne 0) { throw 'live-local health/readiness check failed' }
    Write-Host ('S11 live-local artifacts: ' + $outDir)
} finally {
    if ($null -ne $serverProcess -and -not $serverProcess.HasExited) {
        Stop-Process -Id $serverProcess.Id -Force
    }
    Pop-Location
}
