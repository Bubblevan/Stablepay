$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $repo 'commerce-runtime'
$outDir = Join-Path $repo '.local-run\s11-live-local'
$suiteExe = Join-Path $outDir 's11-live-local-suite.exe'
$evalExe = Join-Path $outDir 'stablepay-agent-eval.exe'
$cliExe = Join-Path $outDir 'stablepay-runtime.exe'

New-Item -ItemType Directory -Force -Path $outDir | Out-Null
Push-Location $runtimeDir
try {
    go build -o $suiteExe ./cmd/s11-live-local-suite
    go build -o $evalExe ./cmd/stablepay-agent-eval
    go build -o $cliExe ./cmd/stablepay-runtime
    & $suiteExe --dataset (Join-Path $runtimeDir 'testdata\s11\live-local\scenarios.jsonl') --out-dir $outDir --runtime-cli $cliExe --trials 8 --poll 50ms --timeout 45s
    if ($LASTEXITCODE -ne 0) { throw 'isolated live-local suite failed' }
    Write-Host ('S11 live-local artifacts: ' + $outDir)
} finally {
    Pop-Location
}
