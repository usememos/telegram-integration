#!/bin/sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
OUTPUT_DIR="$ROOT_DIR/build"
OUTPUT_BIN="$OUTPUT_DIR/memogram-freebsd-amd64"

mkdir -p "$OUTPUT_DIR"

echo "Building memogram for freebsd/amd64..."
CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o "$OUTPUT_BIN" ./bin/memogram/main.go

echo "Build successful: $OUTPUT_BIN"
