#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC_DIR="$(dirname "$SCRIPT_DIR")"

usage() {
    echo "Usage: $(basename "$0") [target]"
    echo ""
    echo "Targets:"
    echo "  build        Build binary + frontend (default)"
    echo "  dev          Run Go + Vite dev servers concurrently"
    echo "  test         Run Go + frontend tests"
    echo "  lint         Run Go + frontend linters"
    echo "  cross-build  Build for all platforms"
    echo "  check-size   Cross-build and verify binary sizes"
    echo "  clean        Remove build artifacts"
    echo "  install      Build and install to GOPATH/bin"
    echo ""
    echo "Environment:"
    echo "  VERSION      Override version (default: git describe)"
    echo "  GOFLAGS      Extra go build flags"
    exit "${1:-0}"
}

TARGET="${1:-build}"

if [ "$TARGET" = "-h" ] || [ "$TARGET" = "--help" ]; then
    usage 0
fi

cd "$SRC_DIR"

make "$TARGET"
