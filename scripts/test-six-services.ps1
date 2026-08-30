[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$Services = @(
    "api-gateway",
    "did-service",
    "payment-service",
    "query-service",
    "verification-service",
    "blockchain-adapter"
)

foreach ($service in $Services) {
    $servicePath = Join-Path $RepoRoot $service
    Write-Host "[six-services] testing $service"
    Push-Location $servicePath
    try {
        & go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw "$service tests failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }
}

& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $RepoRoot "stablepayai-idl\scripts\verify-service-contracts.ps1")
if ($LASTEXITCODE -ne 0) {
    throw "service contract verification failed with exit code $LASTEXITCODE"
}

Write-Host "[six-services] unit tests and contract checks passed"
