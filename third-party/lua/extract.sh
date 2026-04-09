#!/bin/sh
# Build the Lua container and extract the binaries.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/gaston-lua"

echo "Building Lua container (platform: linux/arm64)..."
echo "(requires iansmith/gaston-picolibc image — run third-party/picolibc/extract.sh first)"
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
echo "Binaries:"
ls -lh "$OUTPUT/bin/"
