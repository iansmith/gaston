# MicroPython ARM64 Linux source preparation container
#
# PURPOSE: Build MicroPython natively on ARM64 Linux to:
#   1. Generate all build-time headers (genhdr/ - qstr tables, module defs, etc.)
#   2. Produce a clean source tree ready for gaston to compile on mazarin
#
# USAGE:
#   docker build --platform linux/arm64 -t micropython-src .
#   docker run --rm -v $(pwd)/output:/output micropython-src cp -r /output/. /output/
#
# Or to extract the output directory:
#   id=$(docker create --platform linux/arm64 micropython-src)
#   docker cp $id:/output ./output
#   docker rm $id
#
# OUTPUT LAYOUT (at /output inside container):
#   src/
#     py/           - MicroPython core VM and runtime
#     extmod/       - Extended modules
#     shared/       - Shared utilities (libc wrappers, etc.)
#     ports/unix/   - Unix port C files (main.c, etc.)
#     lib/          - Third-party libraries (libffi, berkeley-db)
#     genhdr/       - ALL generated headers (from build-standard/genhdr/)
#   bin/
#     micropython   - Reference ARM64 Linux binary (proves the sources compile)
#
# INCLUDE PATHS for gaston:
#   -I /path/to/src -I /path/to/src/ports/unix
#
# The genhdr/ directory sits at src/genhdr/ so that
# #include "genhdr/qstrdefs.generated.h" resolves with -I src/

FROM --platform=linux/arm64 ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    build-essential \
    git \
    python3 \
    pkg-config \
    libffi-dev \
    && rm -rf /var/lib/apt/lists/*

ARG MICROPYTHON_VERSION=v1.24.1

# Clone at a pinned version for reproducibility
RUN git clone --depth=1 --branch ${MICROPYTHON_VERSION} \
    https://github.com/micropython/micropython.git /micropython

WORKDIR /micropython

# Initialize only the submodules the unix minimal variant needs.
# We skip mbedtls (SSL/crypto) — not needed for mazarin target.
RUN git submodule update --init --depth=1 \
    lib/micropython-lib \
    lib/berkeley-db-1.xx

# Build mpy-cross first — this is the host-side Python bytecode compiler
# needed to freeze Python modules into the binary
RUN make -C mpy-cross -j$(nproc)

# Build the unix minimal variant for ARM64 Linux.
# "minimal" skips SSL, crypto, FFI, Bluetooth — appropriate for mazarin.
# This runs all the Python code-generation scripts and produces:
#   ports/unix/build-minimal/genhdr/qstrdefs.generated.h
#   ports/unix/build-minimal/genhdr/qstrdefs.preprocessed.h
#   ports/unix/build-minimal/genhdr/moduledefs.h
#   ports/unix/build-minimal/genhdr/root_pointers.h
#   ports/unix/build-minimal/micropython  (final binary)
RUN make -C ports/unix VARIANT=minimal -j$(nproc)

# Assemble the output tree for extraction.
# genhdr/ is placed at the src root so that #include "genhdr/foo.h"
# resolves naturally when compiling with -I src/
RUN set -e && \
    mkdir -p /output/src/ports /output/bin && \
    \
    # Source directories needed to compile micropython
    cp -r py         /output/src/ && \
    cp -r extmod     /output/src/ && \
    cp -r shared     /output/src/ && \
    cp -r ports/unix /output/src/ports/ && \
    cp -r lib        /output/src/ && \
    \
    # Generated headers from the build
    cp -r ports/unix/build-minimal/genhdr /output/src/ && \
    \
    # Reference binary — proves the sources compile cleanly
    cp ports/unix/build-minimal/micropython /output/bin/

WORKDIR /output

# Default command: show what was built
CMD ["/bin/sh", "-c", "echo 'MicroPython ARM64 source tree (minimal variant) ready at /output' && echo '' && echo 'Binary:' && ls -lh /output/bin/ && echo '' && echo 'Generated headers:' && ls /output/src/genhdr/"]
