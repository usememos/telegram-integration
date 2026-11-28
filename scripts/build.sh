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
ldflags="-s -w -X github.com/usememos/memogram/internal/version.Version=$MEMOGRAM_VERSION -X github.com/usememos/memogram/internal/version.Commit=$MEMOGRAM_COMMIT"

build_target=${1:-binary}
if [ "$#" -gt 0 ]; then shift; fi
case "$build_target" in
  binary)
    case "$(uname -s)" in
      *CYGWIN*|*MINGW*|*MSYS*) build_output="./build/memogram.exe" ;;
      *) build_output="./build/memogram" ;;
    esac
    go build -trimpath -ldflags="$ldflags" -o "$build_output" ./cmd/memogram
    printf 'Built %s (%s) at %s\n' "$MEMOGRAM_VERSION" "$MEMOGRAM_COMMIT" "$build_output"
    ;;
  release)
    # One release archive for GOOS/GOARCH (and GOARM), named like the assets on
    # the Releases page: a .zip for Windows and a .tar.gz elsewhere.
    output_dir=$(mkdir -p "${1:-build}" && cd "${1:-build}" && pwd)
    goos=$(go env GOOS)
    goarch=$(go env GOARCH)
    package="memogram_${MEMOGRAM_VERSION}_${goos}_${goarch}"
    if [ "$goarch" = arm ]; then
      package="${package}v$(go env GOARM)"
    fi
    binary=memogram
    if [ "$goos" = windows ]; then
      binary=memogram.exe
    fi
    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT
    CGO_ENABLED=0 go build -trimpath -ldflags="$ldflags" -o "$work/$binary" ./cmd/memogram
    if [ "$goos" = windows ]; then
      archive="$output_dir/$package.zip"
      rm -f "$archive"
      (cd "$work" && zip -q "$archive" "$binary")
    else
      archive="$output_dir/$package.tar.gz"
      # COPYFILE_DISABLE keeps macOS tar from adding "._" metadata entries.
      COPYFILE_DISABLE=1 tar -czf "$archive" -C "$work" "$binary"
    fi
    printf 'Packaged %s (%s) at %s\n' "$MEMOGRAM_VERSION" "$MEMOGRAM_COMMIT" "$archive"
    ;;
  docker)
    docker build --build-arg "VERSION=$MEMOGRAM_VERSION" --build-arg "COMMIT=$MEMOGRAM_COMMIT" "$@" .
    ;;
  compose)
    docker compose "$@"
    ;;
  *) echo 'Usage: sh scripts/build.sh [binary|release [DIR]|docker OPTIONS|compose OPTIONS]' >&2; exit 1 ;;
esac
