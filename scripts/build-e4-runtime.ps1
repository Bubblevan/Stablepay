param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [string]$OutputPath = $(if ($env:E4_RUNTIME_OUTPUT) { $env:E4_RUNTIME_OUTPUT } else { (Join-Path $RepoRoot '.local-run/resume-benchmark/e4-chaos/commerce-runtime.exe') })
)

$ErrorActionPreference = 'Stop'
$runtimeRoot = Join-Path $RepoRoot 'commerce-runtime'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $OutputPath) | Out-Null
Push-Location $runtimeRoot
try {
  & go build -trimpath -o $OutputPath ./cmd/server
  if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
} finally {
  Pop-Location
}
Write-Output (Resolve-Path -LiteralPath $OutputPath).Path
