param(
  [string]$OutDir = ""
)

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
if ([string]::IsNullOrWhiteSpace($OutDir)) {
  $OutDir = Join-Path $RootDir "gen-go"
}

function Fail($msg) {
  Write-Error $msg
  exit 1
}

if (-not (Get-Command thriftgo -ErrorAction SilentlyContinue)) {
  Fail "[stablepayai-idl] 未找到 thriftgo，请先安装 thriftgo（CloudWeGo 生态 IDL 编译器）。"
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
Write-Host "[stablepayai-idl] 输出目录：$OutDir"

$thriftFiles = @(
  (Join-Path $RootDir "idl\\did-service.thrift"),
  (Join-Path $RootDir "idl\\payment-service.thrift"),
  (Join-Path $RootDir "idl\\verification-service.thrift"),
  (Join-Path $RootDir "idl\\query-service.thrift"),
  (Join-Path $RootDir "idl\\blockchain-adapter.thrift")
)

foreach ($f in $thriftFiles) {
  Write-Host "[stablepayai-idl] thriftgo 生成：$(Split-Path $f -Leaf)"
  thriftgo -g go -o $OutDir $f
  if ($LASTEXITCODE -ne 0) {
    Fail "[stablepayai-idl] thriftgo 生成失败：$f"
  }
}

Write-Host "[stablepayai-idl] 完成。"
Write-Host "[stablepayai-idl] 提示：在微服务仓库中建议使用 kitex 生成服务端/客户端代码，例如："
Write-Host "  kitex -module <your-module> -service payment-service -thrift <path-to>/stablepayai-idl/idl/payment-service.thrift"

