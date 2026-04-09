# Picolibc Build — ARM64 Linux container
#
# PURPOSE: Compile picolibc (with gaston patches) into libgastonc.a,
# collect headers, and build gaston-mazarin (gaston with baked-in defaults).
#
# Picolibc 1.8.6 is downloaded from GitHub and patched with
# gaston-picolibc.patch (adds missing includes, FALLTHROUGH annotations,
# code fixes, and the curated libm/include header set).
#
# BUILD CONTEXT: Must be the repository root (extract.sh handles this).
#
# USAGE:
#   ./extract.sh [output-dir]
#
# OUTPUT LAYOUT (at /output inside container):
#   lib/
#     libgastonc.a    - Static library (picolibc + libm + gaston libc)
#   include/          - Headers for use with gaston
#   bin/
#     gaston-mazarin  - Gaston compiler with /gaston/include and /gaston/lib defaults

ARG TARGETPLATFORM=linux/arm64
FROM --platform=$TARGETPLATFORM iansmith/gaston AS base

# Install Go (needed for gastonlibc and gaston-mazarin build)
COPY --from=golang:1.25 /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:$PATH"

# Install patch (needed to apply gaston-picolibc.patch)
RUN apt-get update && apt-get install -y patch && rm -rf /var/lib/apt/lists/*

# Clean inherited /output from base gaston image (we'll build our own output)
RUN rm -rf /output

# Download picolibc 1.8.6 and extract only the directories gastonlibc needs
ARG PICOLIBC_VERSION=1.8.6
RUN curl -sL https://github.com/picolibc/picolibc/archive/refs/tags/${PICOLIBC_VERSION}.tar.gz | tar xz -C /tmp

# Set up the workspace with the flat layout gastonlibc expects.
# gastonlibc uses gastonDir = "." so sources are at workspace root:
#   picolibc/libc/{tinystdio,string,ctype,...,include}
#   libm/{math,common}
#   libc/ (gaston's own libc)
WORKDIR /workspace

# Arrange pristine picolibc into the flat layout:
RUN mkdir -p picolibc/libc libm && \
    for dir in tinystdio string ctype search misc argz stdlib locale posix signal time include; do \
        cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libc/$dir picolibc/libc/; \
    done && \
    cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libm/math libm/ && \
    cp -r /tmp/picolibc-${PICOLIBC_VERSION}/newlib/libm/common libm/ && \
    rm -rf /tmp/picolibc-${PICOLIBC_VERSION}

# Apply gaston patches (adds missing includes, FALLTHROUGH, code fixes,
# and creates the curated libm/include header set).
# Patch paths have prefix picolibc-pristine/cmd/gaston/ — strip 3 levels.
COPY third-party/picolibc/gaston-picolibc.patch /tmp/gaston-picolibc.patch
RUN patch -p3 < /tmp/gaston-picolibc.patch && rm /tmp/gaston-picolibc.patch

# Create a standalone go.mod for this build
RUN go mod init gaston-build

# Copy gaston Go source (needed to build gaston-mazarin)
COPY *.go ./

# Copy gastonlibc Go source (needed to run gastonlibc)
COPY cmd/gastonlibc/*.go cmd/gastonlibc/

# Copy gaston's own libc sources (errno.c, printf.c, etc. — these are
# gaston source, not picolibc, so they're always copied from the repo)
COPY libc/ libc/

# Create a go-wrapper that routes "go tool gaston ..." to the real gaston binary.
# gastonlibc calls: exec.Command(goPath, "tool", "gaston", "-c", ...)
# In the real repo this works because gaston is a Go module tool dependency.
# Here we intercept "tool gaston" and forward to /usr/local/bin/gaston.
RUN printf '#!/bin/sh\n\
if [ "$1" = "tool" ] && [ "$2" = "gaston" ]; then\n\
    shift 2\n\
    exec /usr/local/bin/gaston "$@"\n\
fi\n\
exec /usr/local/go/bin/go "$@"\n' > /usr/local/bin/go-gastonlibc && \
    chmod +x /usr/local/bin/go-gastonlibc

# Build libgastonc.a using gastonlibc
RUN mkdir -p /output/lib && \
    go run ./cmd/gastonlibc -go /usr/local/bin/go-gastonlibc -o /output/lib/libgastonc.a

# Collect headers:
#   1. Copy ALL picolibc headers first (base layer — includes _ansi.h, sys/, etc.)
#   2. Overlay libm/include on top (curated/extended headers with gaston additions)
# Where both have a file, libm/include wins (it has the gaston-patched versions).
RUN mkdir -p /output/include && \
    cp -r picolibc/libc/include/* /output/include/ && \
    cp -r libm/include/* /output/include/

# Build gaston-mazarin (gaston with baked-in /gaston/include and /gaston/lib defaults)
RUN mkdir -p /output/bin && \
    GOOS=linux GOARCH=arm64 go build \
    -ldflags="-X main.defaultIncludes=/gaston/include -X main.defaultLibPaths=/gaston/lib -X main.defaultLibs=gastonc" \
    -o /output/bin/gaston-mazarin .

WORKDIR /output

CMD ["/bin/sh", "-c", "echo 'Picolibc build artifacts ready at /output' && echo '' && echo 'Library:' && ls -lh /output/lib/ && echo '' && echo 'Binary:' && ls -lh /output/bin/ && echo '' && echo 'Headers:' && ls /output/include/ | head -20"]
