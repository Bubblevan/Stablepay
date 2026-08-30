$ErrorActionPreference = "Stop"

$env:GOCACHE = Join-Path $PSScriptRoot "..\.gocache"
$env:GOMODCACHE = Join-Path $PSScriptRoot "..\.gomodcache"

go run ./cmd/api-gateway -config configs/config.yaml
