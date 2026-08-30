#!/bin/bash
# 在 WSL / Linux 下验证「链上 USDC 余额查询」链路（query-service → 香港代理 RPC）
# 用法: ./scripts/verify-balance.sh did:solana:你的主网钱包地址
set -euo pipefail

NAMESPACE="${NAMESPACE:-zheda-agent}"
REGISTRY="${REGISTRY:-stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev}"
AGENT_DID="${1:-}"
# 临时 Pod 需 acr-secret 才能拉 ACR 私有镜像（与 Deployment 一致）
PULL_OVERRIDES='{"spec":{"imagePullSecrets":[{"name":"acr-secret"}]}}'

if [ -z "$AGENT_DID" ]; then
  echo "用法: $0 did:solana:<主网钱包Base58>"
  exit 1
fi

probe_run() {
  local name="$1"
  shift
  kubectl delete pod "$name" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
  kubectl run "$name" -n "$NAMESPACE" --restart=Never \
    --image="${REGISTRY}/curl:8.5.0" \
    --image-pull-policy=IfNotPresent \
    --overrides="$PULL_OVERRIDES" \
    --command -- "$@"
  local i=0
  while [ "$i" -lt 60 ]; do
    phase=$(kubectl get pod "$name" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
    if [ "$phase" = "Succeeded" ] || [ "$phase" = "Failed" ]; then
      break
    fi
    sleep 2
    i=$((i + 1))
  done
  kubectl logs "$name" -n "$NAMESPACE" 2>/dev/null || kubectl describe pod "$name" -n "$NAMESPACE" | tail -20
  kubectl delete pod "$name" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
}

echo "=== 1. Pod 状态 ==="
kubectl get pods -n "$NAMESPACE" \
  -l 'app in (stablepay-query-service,stablepay-blockchain-adapter)'

echo ""
echo "=== 2. blockchain-adapter 配置 ==="
kubectl exec -n "$NAMESPACE" deploy/stablepay-blockchain-adapter -- cat /app/config/cker.yaml || true

echo ""
echo "=== 3. query-service 环境变量 ==="
kubectl exec -n "$NAMESPACE" deploy/stablepay-query-service -- sh -c 'env | grep -E "SOLANA|QUERY_BALANCE" || true'

echo ""
echo "=== 4. 代理 RPC getHealth（ACR curl + acr-secret）==="
probe_run solana-rpc-probe curl -sS -m 30 -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
  'https://proxy.stablepay.co/https://api.mainnet-beta.solana.com'

echo ""
echo "=== 5. query-service /internal/balance ==="
ENC_DID=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$AGENT_DID'))" 2>/dev/null || echo "$AGENT_DID")
probe_run balance-probe curl -sS -m 30 \
  "http://stablepay-query-service:8184/internal/balance?agent_did=${ENC_DID}"

echo ""
echo "=== 6. query-service 日志 [BalanceQuery] ==="
kubectl logs -n "$NAMESPACE" deploy/stablepay-query-service --tail=80 | grep -E 'BalanceQuery|ERROR' || true
