#!/bin/sh
# Build the picolibc container and extract libgastonc.a, headers, and gaston-mazarin.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/gaston-picolibc"

echo "Building picolibc container (platform: linux/arm64)..."
echo "(requires iansmith/gaston image — run third-party/gaston/extract.sh first)"
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
echo "Library:"
ls -lh "$OUTPUT/lib/"
echo ""
echo "Binary:"
ls -lh "$OUTPUT/bin/"
echo ""
echo "Headers:"
ls "$OUTPUT/include/" | head -20
echo "  ... ($(ls "$OUTPUT/include/" | wc -l | tr -d ' ') files total)"
