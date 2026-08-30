[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$IdlRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $IdlRoot "..")

$contracts = @(
    @{ Name = "did-service"; Idl = "did-service.thrift"; Generated = "did-service\kitex_gen\stablepay\did_service\did-service.go"; Methods = @("CreateDID", "RegisterDID", "GetDID", "VerifySignature", "UpdateDIDConfig") },
    @{ Name = "payment-service"; Idl = "payment-service.thrift"; Generated = "payment-service\kitex_gen\stablepay\payment_service\payment-service.go"; Methods = @("InitiatePayment", "GetPaymentStatus", "ListPaymentHistory", "GetPaymentRequirement") },
    @{ Name = "query-service"; Idl = "query-service.thrift"; Generated = "query-service\kitex_gen\stablepay\query_service\query-service.go"; Methods = @("GetBalanceSummary", "ListTransactions", "GetRevenueSummary", "ListSales") },
    @{ Name = "verification-service"; Idl = "verification-service.thrift"; Generated = "verification-service\kitex_gen\stablepay\verification_service\verification-service.go"; Methods = @("VerifyPurchase", "BatchVerifyPurchase", "GetPurchaseProof") },
    @{ Name = "blockchain-adapter"; Idl = "blockchain-adapter.thrift"; Generated = "blockchain-adapter\kitex_gen\stablepay\blockchain_adapter\blockchain-adapter.go"; Methods = @("TransferStableCoin", "GetBalance", "GetTxStatus", "BuildUnsignedTransaction", "SubmitSignedTransaction") }
)

foreach ($contract in $contracts) {
    $idlPath = Join-Path $IdlRoot ("idl\" + $contract.Idl)
    $generatedPath = Join-Path $RepoRoot $contract.Generated
    if (-not (Test-Path -LiteralPath $idlPath -PathType Leaf)) { throw "Missing canonical IDL: $idlPath" }
    if (-not (Test-Path -LiteralPath $generatedPath -PathType Leaf)) { throw "Missing generated contract: $generatedPath" }

    $idlText = Get-Content -LiteralPath $idlPath -Raw
    $generatedText = Get-Content -LiteralPath $generatedPath -Raw
    foreach ($method in $contract.Methods) {
        if ($idlText -notmatch ("\b" + [regex]::Escape($method) + "\s*\(")) { throw "$($contract.Name) IDL is missing $method" }
        if ($generatedText -notmatch ("(?m)^\s+" + [regex]::Escape($method) + "\(ctx context\.Context")) { throw "$($contract.Name) generated interface is missing $method" }
    }
}

$legacyCopies = @(
    "api-gateway\stablepayai-idl\idl",
    "api-gateway\stablepay\common",
    "api-gateway\stablepay\verification_service",
    "payment-service\idl",
    "query-service\idl",
    "verification-service\idl",
    "blockchain-adapter\idl"
)
foreach ($relativePath in $legacyCopies) {
    $legacyPath = Join-Path $RepoRoot $relativePath
    if (Test-Path -LiteralPath $legacyPath -PathType Container) {
        $legacyFiles = @(Get-ChildItem -LiteralPath $legacyPath -Recurse -File)
        if ($legacyFiles.Count -gt 0) { throw "Legacy generated/IDL copy must be removed: $legacyPath" }
    }
}

Write-Host "[stablepayai-idl] all service contracts verified: $($contracts.Name -join ', ')"
