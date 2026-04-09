# Gaston C Compiler — ARM64 Linux container
#
# PURPOSE: Build the gaston compiler and gastonlibc build tool as ARM64 Linux
# binaries. This is the foundation layer for all other third-party containers.
#
# BUILD CONTEXT: Must be the repository root (extract.sh handles this).
#
# USAGE:
#   ./extract.sh [output-dir]
#
# OUTPUT LAYOUT (at /output inside container):
#   bin/
#     gaston         - Gaston compiler (no libc defaults)
#     gastonlibc     - Picolibc build tool
#
# Both gaston and gastonlibc use only Go stdlib — no external dependencies.

ARG TARGETPLATFORM=linux/arm64
FROM --platform=$TARGETPLATFORM golang:1.25 AS builder

WORKDIR /build

# Create a standalone go.mod (gaston and gastonlibc use only Go stdlib)
RUN go mod init gaston-build

# Copy the gaston compiler source (Go files only — not testdata)
COPY *.go ./

# Copy the gastonlibc build tool source
COPY cmd/gastonlibc/*.go cmd/gastonlibc/

# Build gaston compiler (plain, no libc defaults)
RUN go build -o /output/bin/gaston .

# Build gastonlibc tool
RUN go build -o /output/bin/gastonlibc ./cmd/gastonlibc

# --- Final image: slim runtime with just the binaries ---
FROM --platform=$TARGETPLATFORM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /output/bin/gaston /usr/local/bin/gaston
COPY --from=builder /output/bin/gastonlibc /usr/local/bin/gastonlibc

# Also keep copies in /output for extraction
RUN mkdir -p /output/bin && \
    cp /usr/local/bin/gaston /output/bin/ && \
    cp /usr/local/bin/gastonlibc /output/bin/

WORKDIR /output

CMD ["/bin/sh", "-c", "echo 'Gaston ARM64 Linux binaries ready at /output' && ls -lh /output/bin/"]
