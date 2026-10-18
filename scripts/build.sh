#!/bin/sh
set -eu
cd "$(dirname "$0")/../"

# Development versions come from HEAD, not the clock of the build machine.
if [ -n "${MEMOGRAM_VERSION:-}" ]; then
  bash scripts/release_version.sh version "$MEMOGRAM_VERSION" >/dev/null
else
  MEMOGRAM_VERSION=$(bash scripts/release_version.sh development-version)
fi
MEMOGRAM_COMMIT=$(git rev-parse HEAD)
export MEMOGRAM_VERSION MEMOGRAM_COMMIT

build_target=${1:-binary}
if [ "$#" -gt 0 ]; then shift; fi
case "$build_target" in
  binary)
    case "$(uname -s)" in
      *CYGWIN*|*MINGW*|*MSYS*) build_output="./build/memogram.exe" ;;
      *) build_output="./build/memogram" ;;
    esac
    go build -trimpath -ldflags="-s -w -X github.com/usememos/memogram/internal/version.Version=$MEMOGRAM_VERSION -X github.com/usememos/memogram/internal/version.Commit=$MEMOGRAM_COMMIT" -o "$build_output" ./cmd/memogram
    printf 'Built %s (%s) at %s\n' "$MEMOGRAM_VERSION" "$MEMOGRAM_COMMIT" "$build_output"
    ;;
  docker)
    docker build --build-arg "VERSION=$MEMOGRAM_VERSION" --build-arg "COMMIT=$MEMOGRAM_COMMIT" "$@" .
    ;;
  compose)
    docker compose "$@"
    ;;
  *) echo 'Usage: sh scripts/build.sh [binary|docker OPTIONS|compose OPTIONS]' >&2; exit 1 ;;
esac
