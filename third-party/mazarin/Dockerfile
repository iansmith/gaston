# Mazarin C Toolchain — multi-platform container
#
# PURPOSE: Produce the complete C toolchain for mazarin disk image construction:
#   gaston-mazarin  — C compiler with /gaston/include and /gaston/lib baked in
#   libgastonc.a    — C library (picolibc + gaston libc), always AArch64 target
#   include/        — Headers
#
# PLATFORMS: linux/arm64 and linux/amd64.
#   gaston always emits AArch64 code regardless of host, so libgastonc.a and
#   headers are identical across platforms.  Only gaston-mazarin differs: it is
#   a native binary for the host platform so it runs without emulation.
#
# Self-contained: builds gaston from source, downloads picolibc 1.8.6,
# applies the patch, and compiles everything in one pass.
# No pre-built base images required.
#
# BUILD CONTEXT: Must be the repository root (extract.sh handles this).
#
# USAGE:
#   ./extract.sh [output-dir]          # builds for both arm64 and amd64
#
# OUTPUT LAYOUT (per-platform subdirectory inside output-dir):
#   bin/gaston-mazarin  — native C compiler for this platform
#   lib/libgastonc.a    — AArch64 C library (identical across platforms)
#   include/            — Headers (identical across platforms)

# BUILDPLATFORM = the machine running Docker (used for fast native Go builds).
# TARGETPLATFORM = the platform the final image/binary is for (arm64 or amd64).
ARG BUILDPLATFORM
ARG TARGETPLATFORM


# ── Stage 1: Build all Go binaries natively on the build machine ─────────────
#
# Go cross-compilation is trivial: a single native Go toolchain can produce
# binaries for any GOOS/GOARCH.  We build gaston-mazarin for BOTH arm64 and
# amd64 here, and arm64 gaston + gastonlibc for the picolibc compile stage.
FROM --platform=$BUILDPLATFORM golang:1.25 AS go-builder

WORKDIR /build
RUN go mod init gaston-build

COPY *.go ./
COPY cmd/gastonlibc/*.go cmd/gastonlibc/

# gaston and gastonlibc — always arm64 because the picolibc compile stage
# (Stage 2) runs on linux/arm64 and needs arm64-native binaries.
RUN GOOS=linux GOARCH=arm64 go build -o /out/arm64/gaston .
RUN GOOS=linux GOARCH=arm64 go build -o /out/arm64/gastonlibc ./cmd/gastonlibc

# gaston-mazarin for arm64 hosts
RUN GOOS=linux GOARCH=arm64 go build \
    -ldflags="-X main.defaultIncludes=/gaston/include \
              -X main.defaultLibPaths=/gaston/lib \
              -X main.defaultLibs=gastonc" \
    -o /out/arm64/gaston-mazarin .

# gaston-mazarin for amd64 hosts
# (gaston still emits AArch64 code; the binary just runs natively on amd64)
RUN GOOS=linux GOARCH=amd64 go build \
    -ldflags="-X main.defaultIncludes=/gaston/include \
              -X main.defaultLibPaths=/gaston/lib \
              -X main.defaultLibs=gastonc" \
    -o /out/amd64/gaston-mazarin .


# ── Stage 2: Compile picolibc into libgastonc.a (always AArch64 target) ──────
#
# Pinned to linux/arm64 so the arm64 gaston binary runs natively here.
# On an amd64 build machine Docker uses QEMU for this stage only.
FROM --platform=linux/arm64 ubuntu:22.04 AS picolibc-builder

RUN apt-get update && apt-get install -y curl patch && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /out/arm64/gaston     /usr/local/bin/gaston
COPY --from=go-builder /out/arm64/gastonlibc /usr/local/bin/gastonlibc

# Wrapper: gastonlibc calls `$go tool gaston -c|-ar ...`
# Strip "tool gaston" and forward directly to the gaston binary.
RUN printf '#!/bin/sh\n\
if [ "$1" = "tool" ] && [ "$2" = "gaston" ]; then\n\
    shift 2\n\
    exec /usr/local/bin/gaston "$@"\n\
fi\n\
exit 1\n' > /usr/local/bin/go-gaston && chmod +x /usr/local/bin/go-gaston

# Download picolibc 1.8.6 and arrange into the flat layout gastonlibc expects:
#   picolibc/libc/{tinystdio,string,ctype,...,include}
#   libm/{math,common}
ARG PICOLIBC_VERSION=1.8.6
RUN curl -sL https://github.com/picolibc/picolibc/archive/refs/tags/${PICOLIBC_VERSION}.tar.gz \
    | tar xz -C /tmp

WORKDIR /workspace

RUN mkdir -p picolibc/libc libm && \
    for dir in tinystdio string ctype search misc argz stdlib locale posix signal time include; do \
        cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libc/$dir picolibc/libc/; \
    done && \
    cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libm/math   libm/ && \
    cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libm/common libm/ && \
    rm -rf /tmp/picolibc-${PICOLIBC_VERSION}

# Apply the gaston patch (adds missing includes, correctness fixes,
# the curated libm/include header set, and FALLTHROUGH annotations).
COPY third-party/picolibc/gaston-picolibc.patch /tmp/gaston-picolibc.patch
RUN patch -p3 < /tmp/gaston-picolibc.patch && rm /tmp/gaston-picolibc.patch

# Copy gaston's own libc sources (errno.c, printf.c, etc.)
COPY libc/ libc/

# Compile everything into libgastonc.a
RUN mkdir -p /output/lib && \
    gastonlibc -go /usr/local/bin/go-gaston -o /output/lib/libgastonc.a

# Collect headers:
#   1. picolibc base headers (sys/, machine/, etc.)
#   2. libm/include overlay (curated headers — wins on conflict)
RUN mkdir -p /output/include && \
    cp -r picolibc/libc/include/* /output/include/ && \
    cp -r libm/include/*          /output/include/


# ── Final image: platform-specific gaston-mazarin + shared lib + headers ──────
#
# scratch base: the exported filesystem contains ONLY the toolchain files,
# so `docker buildx --output type=local` produces a clean output directory.
FROM scratch

ARG TARGETARCH

COPY --from=go-builder       /out/${TARGETARCH}/gaston-mazarin /bin/gaston-mazarin
COPY --from=picolibc-builder /output/lib/libgastonc.a          /lib/libgastonc.a
COPY --from=picolibc-builder /output/include                   /include
