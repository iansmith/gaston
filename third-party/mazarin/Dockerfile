# Mazarin C Toolchain — AArch64 Linux container
#
# PURPOSE: Produce the complete C toolchain for mazarin disk image construction:
#   gaston-mazarin  — AArch64 C compiler with /gaston/include and /gaston/lib baked in
#   libgastonc.a    — C library (picolibc + gaston libc)
#   include/        — Headers
#
# Self-contained: builds gaston from source, downloads picolibc 1.8.6,
# applies the patch, and compiles everything in one pass.
# No pre-built base images required.
#
# BUILD CONTEXT: Must be the repository root (extract.sh handles this).
#
# USAGE:
#   ./extract.sh [output-dir]
#
# OUTPUT LAYOUT (output-dir on host, /output inside container):
#   bin/gaston-mazarin  — AArch64 C compiler
#   lib/libgastonc.a    — AArch64 C library
#   include/            — Headers

# BUILDPLATFORM = the machine running Docker.
# Go cross-compilation is used so Stage 1 always runs natively (no QEMU),
# regardless of whether the build machine is arm64 or amd64.
ARG BUILDPLATFORM


# ── Stage 1: Build Go binaries natively on the build machine ─────────────────
FROM --platform=$BUILDPLATFORM golang:1.25 AS go-builder

WORKDIR /build
RUN go mod init gaston-build

COPY *.go ./
COPY cmd/gastonlibc/*.go cmd/gastonlibc/

# All three binaries target linux/arm64 (the mazarin target).
RUN GOOS=linux GOARCH=arm64 go build -o /out/gaston .
RUN GOOS=linux GOARCH=arm64 go build -o /out/gastonlibc ./cmd/gastonlibc
RUN GOOS=linux GOARCH=arm64 go build \
    -ldflags="-X main.defaultIncludes=/gaston/include \
              -X main.defaultLibPaths=/gaston/lib \
              -X main.defaultLibs=gastonc" \
    -o /out/gaston-mazarin .


# ── Stage 2: Compile picolibc into libgastonc.a and collect headers ───────────
FROM --platform=linux/arm64 ubuntu:22.04 AS picolibc-builder

RUN apt-get update && apt-get install -y curl patch && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /out/gaston     /usr/local/bin/gaston
COPY --from=go-builder /out/gastonlibc /usr/local/bin/gastonlibc

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


# ── Final image: toolchain artifacts only ────────────────────────────────────
FROM scratch

COPY --from=go-builder       /out/gaston-mazarin        /bin/gaston-mazarin
COPY --from=picolibc-builder /output/lib/libgastonc.a   /lib/libgastonc.a
COPY --from=picolibc-builder /output/include            /include
