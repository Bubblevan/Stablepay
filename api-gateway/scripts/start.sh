#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="$(pwd)/.gocache"
export GOMODCACHE="$(pwd)/.gomodcache"

go run ./cmd/server -config config/config.yaml
