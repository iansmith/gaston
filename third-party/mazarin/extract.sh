#!/bin/sh
# Build the mazarin C toolchain and extract artifacts to the host.
#
# Produces gaston (AArch64), libgastonc.a, and headers —
# everything needed to compile C programs for baking into a mazarin disk image.
#
# Also tags the image as iansmith/mazarin-toolchain so that third-party
# application builds (lua, etc.) can use it as their base.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/mazarin-toolchain"

echo "Building mazarin toolchain (AArch64)..."
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
echo "Image tagged:    $IMAGE"
echo ""
echo "Compiler: $(ls -lh "$OUTPUT/bin/gaston" | awk '{print $5, $9}')"
echo "Library:  $(ls -lh "$OUTPUT/lib/libgastonc.a" | awk '{print $5, $9}')"
echo "Headers:  $(ls "$OUTPUT/include/" | wc -l | tr -d ' ') files"
