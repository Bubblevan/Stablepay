#!/bin/bash
# 在 ACK 内清空购买/支付/验证相关 MySQL 表（保留 DID 注册）
# 用法: ./reset-mysql-purchase-state.sh
# 环境: NS=zheda-agent MYSQL_POD=stablepay-mysql（可覆盖）

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NS="${NS:-zheda-agent}"
MYSQL_DEPLOY="${MYSQL_DEPLOY:-stablepay-mysql}"
MYSQL_USER="${MYSQL_USER:-stablepay}"
MYSQL_PASS="${MYSQL_PASS:-stablepay123}"

echo "=== Reset purchase/payment/verify tables (namespace: ${NS}) ==="
echo "DID 表 did_identities 不会动。"
echo ""

kubectl exec -n "$NS" "deploy/${MYSQL_DEPLOY}" -- \
  mysql -u"$MYSQL_USER" -p"$MYSQL_PASS" < "${SCRIPT_DIR}/reset-mysql-purchase-state.sql"

echo ""
echo "Done. Redeploy verification-service if MQ consumer was broken, then pay again."
