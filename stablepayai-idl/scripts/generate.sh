#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

OUT_DIR="${1:-$ROOT_DIR/gen-go}"

if ! command -v thriftgo >/dev/null 2>&1; then
  echo "[stablepayai-idl] 未找到 thriftgo，请先安装 thriftgo（CloudWeGo 生态 IDL 编译器）。" >&2
  echo "[stablepayai-idl] 说明：本脚本仅用于在本仓库验证/生成 Go 类型，微服务仓库建议使用 kitex 生成 RPC 代码。" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

echo "[stablepayai-idl] 输出目录：$OUT_DIR"

THRIFT_FILES=(
  "$ROOT_DIR/idl/did-service.thrift"
  "$ROOT_DIR/idl/payment-service.thrift"
  "$ROOT_DIR/idl/verification-service.thrift"
  "$ROOT_DIR/idl/query-service.thrift"
  "$ROOT_DIR/idl/blockchain-adapter.thrift"
)

for f in "${THRIFT_FILES[@]}"; do
  echo "[stablepayai-idl] thriftgo 生成：$(basename "$f")"
  thriftgo -g go -o "$OUT_DIR" "$f"
done

echo "[stablepayai-idl] 完成。"
echo "[stablepayai-idl] 提示：在微服务仓库中建议使用 kitex 生成服务端/客户端代码，例如："
echo "  kitex -module <your-module> -service payment-service -thrift <path-to>/stablepayai-idl/idl/payment-service.thrift"

