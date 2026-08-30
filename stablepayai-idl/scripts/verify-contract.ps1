[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$IdlRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $IdlRoot "..")
$Canonical = Join-Path $IdlRoot "idl\blockchain-adapter.thrift"
$Generated = Join-Path $RepoRoot "blockchain-adapter\kitex_gen\stablepay\blockchain_adapter\blockchain-adapter.go"
$LegacyCopies = @(
  (Join-Path $RepoRoot "blockchain-adapter\idl\blockchain-adapter.thrift"),
  (Join-Path $RepoRoot "api-gateway\stablepayai-idl\idl\blockchain-adapter.thrift")
)

function Normalize([string]$Content) {
  return (($Content -replace "`r`n", "`n") -replace "[ \t]+`n", "`n").Trim()
}

function Assert-File([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "Required contract file does not exist: $Path"
  }
}

Assert-File $Canonical
Assert-File $Generated

$canonicalText = Normalize (Get-Content -LiteralPath $Canonical -Raw)
$expectedMethods = @(
  "TransferStableCoin",
  "GetBalance",
  "GetTxStatus",
  "BuildUnsignedTransaction",
  "SubmitSignedTransaction"
)

foreach ($method in $expectedMethods) {
  if ($canonicalText -notmatch ("\b" + [regex]::Escape($method) + "\s*\(")) {
    throw "Canonical IDL is missing BlockchainAdapterService method: $method"
  }
}

$methodLines = [regex]::Matches($canonicalText, "(?m)^\s+[A-Za-z][A-Za-z0-9_]*Response\s+([A-Za-z][A-Za-z0-9_]*)\s*\(")
$actualMethods = @($methodLines | ForEach-Object { $_.Groups[1].Value })
if ((Compare-Object $expectedMethods $actualMethods) -ne $null) {
  throw "Canonical IDL method set differs. Expected: $($expectedMethods -join ', '); Actual: $($actualMethods -join ', ')"
}

foreach ($copy in $LegacyCopies) {
  if (Test-Path -LiteralPath $copy -PathType Leaf) {
    throw "Legacy contract copy must be removed, but still exists: $copy"
  }
}

$generatedText = Get-Content -LiteralPath $Generated -Raw
$generatedMethods = @(
  "TransferStableCoin",
  "GetBalance",
  "GetTxStatus",
  "BuildUnsignedTransaction",
  "SubmitSignedTransaction"
)
foreach ($method in $generatedMethods) {
  if ($generatedText -notmatch ("(?m)^\s+" + [regex]::Escape($method) + "\(ctx context\.Context")) {
    throw "Generated blockchain adapter interface is missing method: $method"
  }
}

Write-Host "[stablepayai-idl] blockchain-adapter contract v1 verified: $($expectedMethods -join ', ')"
