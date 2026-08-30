#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 ]]; then
  echo "usage: $0 <route> <expected_status> <output_json> <query_or_dash> [rate] [duration] [preallocated_vus]" >&2
  exit 1
fi

ROUTE_INPUT="$1"
EXPECTED_STATUS_INPUT="$2"
OUTPUT_JSON_INPUT="$3"
QUERY_INPUT="$4"
RATE_INPUT="${5:-5}"
DURATION_INPUT="${6:-10s}"
PREALLOCATED_VUS_INPUT="${7:-5}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="$SCRIPT_DIR/api_baseline.js"

export BASE_URL="${BASE_URL:-https://ai.wenfu.cn}"
export ROUTE="$ROUTE_INPUT"
export EXPECTED_STATUS="$EXPECTED_STATUS_INPUT"
export RATE="$RATE_INPUT"
export DURATION="$DURATION_INPUT"
export PREALLOCATED_VUS="$PREALLOCATED_VUS_INPUT"

if [[ "$QUERY_INPUT" == "-" ]]; then
  export QUERY=""
elif [[ "$QUERY_INPUT" == @* ]]; then
  QUERY_FILE_PATH="${QUERY_INPUT#@}"
  export QUERY="$(cat "$QUERY_FILE_PATH")"
else
  export QUERY="$QUERY_INPUT"
fi

k6 run "--summary-export=$OUTPUT_JSON_INPUT" "$SCRIPT_PATH"
