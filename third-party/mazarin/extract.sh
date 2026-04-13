#!/bin/sh
# Build the mazarin toolchain container and extract artifacts.
#
# Produces gaston-mazarin, libgastonc.a, and headers — everything needed
# to compile C programs for mazarin's disk image.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/mazarin-toolchain"

echo "Building mazarin toolchain (platform: linux/arm64)..."
docker build --platform linux/arm64 \
    -t "$IMAGE" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$REPO_ROOT"

echo "Extracting output to $OUTPUT ..."
rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"

id=$(docker create --platform linux/arm64 "$IMAGE")
docker cp "$id":/output/. "$OUTPUT/"
docker rm "$id"

echo ""
echo "Done. Output at: $OUTPUT"
echo ""
echo "Compiler:"
ls -lh "$OUTPUT/bin/"
echo ""
echo "Library:"
ls -lh "$OUTPUT/lib/"
echo ""
echo "Headers ($(ls "$OUTPUT/include/" | wc -l | tr -d ' ') files):"
ls "$OUTPUT/include/" | head -20
echo "  ..."
