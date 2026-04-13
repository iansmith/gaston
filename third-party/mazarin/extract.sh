#!/bin/sh
# Build the mazarin toolchain for linux/arm64 and linux/amd64, then extract.
#
# Produces gaston-mazarin (platform-native), libgastonc.a (AArch64 target),
# and headers — everything needed to compile C programs for mazarin.
#
# Usage: ./extract.sh [output-dir]
#   output-dir defaults to ./output
#
# Output layout:
#   <output-dir>/
#     linux_arm64/
#       bin/gaston-mazarin   — arm64-native compiler
#       lib/libgastonc.a     — AArch64 C library
#       include/             — headers
#     linux_amd64/
#       bin/gaston-mazarin   — amd64-native compiler
#       lib/libgastonc.a     — AArch64 C library (identical to arm64 copy)
#       include/             — headers (identical to arm64 copy)
#
# Requirements:
#   docker buildx (included in Docker Desktop; on Linux: docker buildx create --use)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT="${1:-$SCRIPT_DIR/output}"
IMAGE="iansmith/mazarin-toolchain"

# Ensure a buildx builder capable of multi-platform builds is active.
# Docker Desktop provides one by default; on plain Docker Engine you may need:
#   docker buildx create --name mazarin-builder --use
if ! docker buildx inspect 2>/dev/null | grep -q "linux/arm64"; then
    echo "WARNING: current buildx builder may not support linux/arm64."
    echo "If the build fails, run: docker buildx create --name mazarin-builder --use"
fi

echo "Building mazarin toolchain (linux/arm64 + linux/amd64)..."
echo "(picolibc stage always runs on linux/arm64; may use QEMU on amd64 hosts)"

rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"

docker buildx build \
    --platform linux/arm64,linux/amd64 \
    --output "type=local,dest=$OUTPUT" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$REPO_ROOT"

echo ""
echo "Done. Output at: $OUTPUT"
echo ""
echo "arm64 compiler:  $(ls -lh "$OUTPUT/linux_arm64/bin/gaston-mazarin" 2>/dev/null | awk '{print $5, $9}' || echo 'not found')"
echo "amd64 compiler:  $(ls -lh "$OUTPUT/linux_amd64/bin/gaston-mazarin" 2>/dev/null | awk '{print $5, $9}' || echo 'not found')"
echo "library:         $(ls -lh "$OUTPUT/linux_arm64/lib/libgastonc.a"   2>/dev/null | awk '{print $5, $9}' || echo 'not found')"
echo "headers:         $(ls "$OUTPUT/linux_arm64/include/" 2>/dev/null | wc -l | tr -d ' ') files"
