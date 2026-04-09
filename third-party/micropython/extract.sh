#!/bin/sh
# Build the MicroPython source-prep container and extract the output.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

OUTPUT="${1:-./output}"
IMAGE="micropython-src"

echo "Building container (platform: linux/arm64)..."
docker build --platform linux/arm64 -t "$IMAGE" "$(dirname "$0")"

echo "Extracting output to $OUTPUT ..."
rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"

id=$(docker create --platform linux/arm64 "$IMAGE")
docker cp "$id":/output/. "$OUTPUT/"
docker rm "$id"

echo ""
echo "Done. Output at: $OUTPUT"
echo ""
echo "Binary:"
ls -lh "$OUTPUT/bin/"
echo ""
echo "Generated headers:"
ls "$OUTPUT/src/genhdr/"
