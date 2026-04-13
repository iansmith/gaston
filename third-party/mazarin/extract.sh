#!/bin/sh
# Build the mazarin C toolchain and extract artifacts to the host.
#
# Produces gaston-mazarin (AArch64), libgastonc.a, and headers —
# everything needed to compile C programs for baking into a mazarin disk image.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/mazarin-toolchain"

echo "Building mazarin toolchain (AArch64)..."
docker buildx build --platform linux/arm64 \
    --output "type=local,dest=$OUTPUT" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$REPO_ROOT"

echo ""
echo "Done. Output at: $OUTPUT"
echo ""
echo "Compiler: $(ls -lh "$OUTPUT/bin/gaston-mazarin" | awk '{print $5, $9}')"
echo "Library:  $(ls -lh "$OUTPUT/lib/libgastonc.a"   | awk '{print $5, $9}')"
echo "Headers:  $(ls "$OUTPUT/include/" | wc -l | tr -d ' ') files"
