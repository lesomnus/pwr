#!/bin/sh
set -eu
# Run from this directory; use Go1.27.1 and Rust1.99.0 for the measured toolchains.
mkdir -p bin
go mod download
go run ./cmd/schema > fields.json
python3 generate.py --ffi-opt
python3 generate_kernel.py
gofmt -w generated.go kernel_generated.go backend_*ffi.go internal/*bridge/*.go
rustc --edition 2024 --crate-type staticlib -C opt-level=3 -C panic=abort -C codegen-units=1 -C lto=thin internal/rbridge/reader.rs -o internal/rbridge/libreader.a
go test -tags all ./...
go build -tags all -trimpath -ldflags '-s -w' -o bin/bench-final ./cmd/bench
go build -tags all -trimpath -ldflags '-s -w' -o bin/extra-final ./cmd/extra
./bin/bench-final -mode check
# Example: taskset -c 2 ./bin/bench-final -duration 100ms -count 3 -seed 436
# Exact measured per-suite commands are in ../../../docs/experiments/2026-10-15/alternatives/results/*.meta.json.
