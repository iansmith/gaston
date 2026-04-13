#!/bin/sh
# Build Lua for the mazarin disk image and extract binaries.
#
# Requires iansmith/mazarin-toolchain to exist locally.
# Run third-party/mazarin/extract.sh first (or: task docker-mazarin).
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/mazarin-lua"

echo "Building Lua 5.4.7 for mazarin (AArch64)..."
docker build --platform linux/arm64 \
    -t "$IMAGE" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$REPO_ROOT"

echo "Extracting output to $OUTPUT ..."
rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"

id=$(docker create --platform linux/arm64 "$IMAGE")
docker cp "$id":/ "$OUTPUT/"
docker rm "$id"

echo ""
echo "Done. Output at: $OUTPUT"
echo ""
echo "Binaries:"
ls -lh "$OUTPUT/bin/"
