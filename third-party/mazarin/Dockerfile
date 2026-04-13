# Mazarin C Toolchain — ARM64 Linux container
#
# PURPOSE: Produce the complete C toolchain for mazarin disk image construction:
#   gaston-mazarin  — C compiler with /gaston/include and /gaston/lib baked in
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
# OUTPUT LAYOUT (at /output inside container, and in output-dir on host):
#   bin/
#     gaston-mazarin  — C compiler (baked defaults: /gaston/include, /gaston/lib, -lgastonc)
#   lib/
#     libgastonc.a    — C library
#   include/          — Headers for use with gaston-mazarin

ARG TARGETPLATFORM=linux/arm64

# ── Stage 1: Build gaston, gastonlibc, and gaston-mazarin from Go source ─────
FROM --platform=$TARGETPLATFORM golang:1.25 AS go-builder

WORKDIR /build
RUN go mod init gaston-build

COPY *.go ./
COPY cmd/gastonlibc/*.go cmd/gastonlibc/

# Plain gaston binary (used internally to compile picolibc C sources)
RUN go build -o /out/gaston .

# gastonlibc — orchestrates the picolibc compilation
RUN go build -o /out/gastonlibc ./cmd/gastonlibc

# gaston-mazarin — gaston with /gaston/include, /gaston/lib, and -lgastonc baked in
RUN go build \
    -ldflags="-X main.defaultIncludes=/gaston/include \
              -X main.defaultLibPaths=/gaston/lib \
              -X main.defaultLibs=gastonc" \
    -o /out/gaston-mazarin .


# ── Stage 2: Compile picolibc into libgastonc.a and collect headers ───────────
FROM --platform=$TARGETPLATFORM ubuntu:22.04 AS picolibc-builder

RUN apt-get update && apt-get install -y curl patch && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /out/gaston     /usr/local/bin/gaston
COPY --from=go-builder /out/gastonlibc /usr/local/bin/gastonlibc

# Wrapper: gastonlibc calls `$go tool gaston -c|-ar ...`
# Strip the "tool gaston" prefix and forward directly to the gaston binary.
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
    cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libm/math  libm/ && \
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
#   2. libm/include overlay (curated headers with gaston additions — wins on conflict)
RUN mkdir -p /output/include && \
    cp -r picolibc/libc/include/* /output/include/ && \
    cp -r libm/include/*          /output/include/


# ── Final image: minimal image holding just the toolchain artifacts ───────────
FROM --platform=$TARGETPLATFORM ubuntu:22.04

COPY --from=go-builder    /out/gaston-mazarin         /output/bin/gaston-mazarin
COPY --from=picolibc-builder /output/lib/libgastonc.a /output/lib/libgastonc.a
COPY --from=picolibc-builder /output/include          /output/include

WORKDIR /output

CMD ["/bin/sh", "-c", "\
    echo 'Mazarin C toolchain ready at /output' && echo '' && \
    echo 'Compiler:' && ls -lh /output/bin/ && echo '' && \
    echo 'Library:'  && ls -lh /output/lib/ && echo '' && \
    echo 'Headers ('$(ls /output/include/ | wc -l | tr -d ' ')' files):' && \
    ls /output/include/ | head -20 && echo '  ...'"]
