#!/bin/sh
# Build the MicroPython source-prep container and extract the output.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output (relative to this script)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="micropython-src"

echo "Building container (platform: linux/arm64)..."
docker build --platform linux/arm64 -t "$IMAGE" "$SCRIPT_DIR"

echo "Extracting output to $OUTPUT ..."
rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"

id=$(docker create --platform linux/arm64 "$IMAGE")
docker cp "$id":/output/. "$OUTPUT/"
docker rm "$id" > /dev/null

echo ""
echo "Done. Output at: $OUTPUT"
echo ""
echo "Binary:"
ls -lh "$OUTPUT/bin/"
echo ""
echo "Generated headers:"
ls "$OUTPUT/src/genhdr/"
