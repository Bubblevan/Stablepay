#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="$(pwd)/.gocache"
export GOMODCACHE="$(pwd)/.gomodcache"

go run ./cmd/api-gateway -config configs/config.yaml
