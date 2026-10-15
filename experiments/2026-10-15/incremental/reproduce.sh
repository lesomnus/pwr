#!/bin/sh
set -eu
mkdir -p bin
gofmt -w incremental.go incremental_test.go scenario.go scenario_test.go cmd/scanbench/main.go
go test -tags 'wire,table,incremental' ./...
go build -tags 'wire,table,incremental' -trimpath -ldflags '-s -w' -o bin/scanbench ./cmd/scanbench
go build -tags 'wire,table' -trimpath -ldflags '-s -w' -o bin/size-base ./cmd/scanbench
./bin/scanbench -mode check
# CPU2 pinning is appropriate only if that logical CPU is available.
# taskset -c 2 ./bin/scanbench -duration 100ms -count 3 -seed 436 > scan.jsonl
# Exact remote commands are in ../../../docs/experiments/2026-10-15/incremental/results/scan-*.meta.json.
