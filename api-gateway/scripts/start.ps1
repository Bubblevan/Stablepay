$ErrorActionPreference = "Stop"

$env:GOCACHE = Join-Path $PSScriptRoot "..\.gocache"
$env:GOMODCACHE = Join-Path $PSScriptRoot "..\.gomodcache"

go run ./cmd/server -config config/config.yaml
