#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REPORT_DIR="$ROOT_DIR/reports/task-02"
RUNNER="$SCRIPT_DIR/run_api_baseline.sh"
SUMMARIZER="$SCRIPT_DIR/summarize_k6.py"

mkdir -p "$REPORT_DIR"

BASE_URL="${BASE_URL:-https://ai.wenfu.cn}"
DURATION="${DURATION:-30s}"
# ===== 修复3: 动态 VU，不再固定 =====
# 如果用户显式设置了 PREALLOCATED_VUS 则使用，否则根据速率自动计算
if [[ -n "${PREALLOCATED_VUS:-}" ]]; then
  FIXED_PREALLOCATED="$PREALLOCATED_VUS"
else
  FIXED_PREALLOCATED=""  # 将在循环内按 rate*2 计算
fi

RATES="${RATES:-20 50 100}"

export BASE_URL

# 定义路由： route|expected_status|query_or_dash
routes=(
  "/healthz|200|-"
  "/readyz|200|-"
  "/api/v1/pay/require|402|skill_did=did:solana:testskill"
  "/verify|200|@$SCRIPT_DIR/queries/verify-short.txt"
)

echo "[task-02] base_url=$BASE_URL duration=$DURATION"
echo "[task-02] rates: $RATES"

# ===== 修复4: 收集失败 case，最后统一返回 =====
failures=()

for rate in $RATES; do
  echo "[task-02] Running rate=$rate RPS"

  # 动态计算 VU: 如果用户未指定，则按 rate*2 预分配
  if [[ -n "$FIXED_PREALLOCATED" ]]; then
    current_preallocated="$FIXED_PREALLOCATED"
  else
    current_preallocated=$((rate * 2))
  fi

  for entry in "${routes[@]}"; do
    IFS='|' read -r route expected query <<< "$entry"
    safe_route="${route//\//_}"
    output="$REPORT_DIR/${safe_route}-summary-rate${rate}.json"

    # ===== 修复4续: 单个 case 失败不退出，继续执行 =====
    if ! bash "$RUNNER" \
      "$route" \
      "$expected" \
      "$output" \
      "$query" \
      "$rate" \
      "$DURATION" \
      "$current_preallocated"; then

      echo "[task-02] WARN: failed route=$route rate=$rate"
      failures+=("${route}@${rate}RPS")
    fi
  done
done

# 汇总所有结果（即使有失败也会生成部分报告）
python3 "$SUMMARIZER" "$REPORT_DIR" $RATES > "$REPORT_DIR/summary-matrix.md"

echo "[task-02] matrix reports written to $REPORT_DIR"
echo "[task-02] matrix summary: $REPORT_DIR/summary-matrix.md"

# 最后统一返回失败状态（如果有任何 case 失败）
if ((${#failures[@]} > 0)); then
  echo "[task-02] failed cases:"
  printf '  - %s\n' "${failures[@]}"
  exit 1
fi