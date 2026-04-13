# Lua 5.4.7 — mazarin disk image build
#
# MODEL: This Dockerfile is the template for building third-party C programs
# for the mazarin disk image.  Every new program follows this same three-stage
# pattern:
#
#   Stage 1 (toolchain): pull gaston + libgastonc.a + headers from the
#                        mazarin toolchain image (iansmith/mazarin-toolchain).
#   Stage 2 (builder):   run on linux/arm64, compile the program with gaston.
#   Final   (scratch):   output binaries only — no OS layer.
#
# The final image contains only what goes into the disk image:
#   /bin/<program>     — one or more AArch64 ELF binaries
#
# BUILD CONTEXT: Must be the repository root (extract.sh handles this).
# PREREQUISITE:  iansmith/mazarin-toolchain must exist locally
#                (run third-party/mazarin/extract.sh first, or task docker-mazarin).
#
# USAGE:
#   ./extract.sh [output-dir]
#
# OUTPUT LAYOUT (output-dir on host → target path on mazarin):
#   bin/lua             → /bin/lua
#   bin/luac            → /bin/luac


# ── Stage 1: pull the mazarin C toolchain ────────────────────────────────────
FROM iansmith/mazarin-toolchain AS toolchain
# Provides: /bin/gaston  /lib/libgastonc.a  /include/


# ── Stage 2: compile Lua with gaston ─────────────────────────────────────────
FROM --platform=linux/arm64 ubuntu:22.04 AS builder

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Install toolchain into the standard locations gaston expects.
COPY --from=toolchain /bin/gaston           /usr/local/bin/gaston
COPY --from=toolchain /lib/libgastonc.a     /gaston/lib/libgastonc.a
COPY --from=toolchain /include              /gaston/include

ARG LUA_VERSION=5.4.7
RUN curl -sL https://www.lua.org/ftp/lua-${LUA_VERSION}.tar.gz | tar xz -C /tmp

WORKDIR /tmp/lua-${LUA_VERSION}/src

# Compile all Lua core + library source files.
RUN set -e && \
    for f in lapi.c lcode.c lctype.c ldebug.c ldo.c ldump.c lfunc.c lgc.c \
             llex.c lmem.c lobject.c lopcodes.c lparser.c lstate.c lstring.c \
             ltable.c ltm.c lundump.c lvm.c lzio.c \
             lauxlib.c lbaselib.c lcorolib.c ldblib.c liolib.c lmathlib.c \
             loadlib.c loslib.c lstrlib.c ltablib.c lutf8lib.c linit.c; do \
        echo "  CC $f" && \
        gaston -I . -I /gaston/include -D LUA_USE_POSIX -c -o $(basename $f .c).o $f; \
    done

# Archive into liblua.a
RUN gaston -ar -o liblua.a \
    lapi.o lcode.o lctype.o ldebug.o ldo.o ldump.o lfunc.o lgc.o \
    llex.o lmem.o lobject.o lopcodes.o lparser.o lstate.o lstring.o \
    ltable.o ltm.o lundump.o lvm.o lzio.o \
    lauxlib.o lbaselib.o lcorolib.o ldblib.o liolib.o lmathlib.o \
    loadlib.o loslib.o lstrlib.o ltablib.o lutf8lib.o linit.o

# Link lua interpreter and luac compiler
RUN gaston -I . -I /gaston/include -D LUA_USE_POSIX -c -o lua.o lua.c && \
    gaston -link -o /output/bin/lua lua.o liblua.a -L /gaston/lib -l gastonc

RUN gaston -I . -I /gaston/include -D LUA_USE_POSIX -c -o luac.o luac.c && \
    gaston -link -o /output/bin/luac luac.o liblua.a -L /gaston/lib -l gastonc


# ── Final image: binaries only ────────────────────────────────────────────────
FROM scratch

COPY --from=builder /output/bin /bin

CMD ["/bin/lua"]
