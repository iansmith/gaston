# Lua Build — ARM64 Linux container
#
# PURPOSE: Download and compile Lua 5.4.7 using gaston, producing a
# standalone Lua interpreter binary for ARM64 Linux.
#
# BUILD CONTEXT: Must be the repository root (extract.sh handles this).
#
# USAGE:
#   ./extract.sh [output-dir]
#
# OUTPUT LAYOUT (at /output inside container):
#   bin/
#     lua             - Lua interpreter (ARM64 Linux ELF)
#     luac            - Lua compiler (ARM64 Linux ELF)

ARG TARGETPLATFORM=linux/arm64
FROM --platform=$TARGETPLATFORM iansmith/gaston-picolibc AS base

ARG LUA_VERSION=5.4.7

# Download Lua source
RUN curl -sL https://www.lua.org/ftp/lua-${LUA_VERSION}.tar.gz | tar xz -C /tmp

WORKDIR /tmp/lua-${LUA_VERSION}/src

# Install the picolibc artifacts where gaston-mazarin expects them.
# gaston-mazarin has defaults: -I/gaston/include -L /gaston/lib -l gastonc
RUN mkdir -p /gaston/include /gaston/lib && \
    cp -r /output/include/* /gaston/include/ && \
    cp /output/lib/libgastonc.a /gaston/lib/

# Clean /output for our results
RUN rm -rf /output && mkdir -p /output/bin

# Compile all Lua core + library source files.
# Lua uses -std=gnu99 and -DLUA_COMPAT_5_3 by default.
# The "generic" target needs no platform-specific defines.
# We add -DLUA_USE_POSIX for basic POSIX support (popen, mkstemp, etc.)
RUN set -e && \
    for f in lapi.c lcode.c lctype.c ldebug.c ldo.c ldump.c lfunc.c lgc.c \
             llex.c lmem.c lobject.c lopcodes.c lparser.c lstate.c lstring.c \
             ltable.c ltm.c lundump.c lvm.c lzio.c \
             lauxlib.c lbaselib.c lcorolib.c ldblib.c liolib.c lmathlib.c \
             loadlib.c loslib.c lstrlib.c ltablib.c lutf8lib.c linit.c; do \
        echo "  CC $f" && \
        /usr/local/bin/gaston -I . -I /gaston/include -D LUA_USE_POSIX -c -o $(basename $f .c).o $f; \
    done && \
    echo "All Lua source files compiled successfully."

# Archive into liblua.a
RUN /usr/local/bin/gaston -ar -o liblua.a \
    lapi.o lcode.o lctype.o ldebug.o ldo.o ldump.o lfunc.o lgc.o \
    llex.o lmem.o lobject.o lopcodes.o lparser.o lstate.o lstring.o \
    ltable.o ltm.o lundump.o lvm.o lzio.o \
    lauxlib.o lbaselib.o lcorolib.o ldblib.o liolib.o lmathlib.o \
    loadlib.o loslib.o lstrlib.o ltablib.o lutf8lib.o linit.o

# Compile and link the lua interpreter
RUN /usr/local/bin/gaston -I . -I /gaston/include -D LUA_USE_POSIX -c -o lua.o lua.c && \
    /usr/local/bin/gaston -link -o /output/bin/lua lua.o liblua.a -L /gaston/lib -l gastonc

# Compile and link the luac compiler
RUN /usr/local/bin/gaston -I . -I /gaston/include -D LUA_USE_POSIX -c -o luac.o luac.c && \
    /usr/local/bin/gaston -link -o /output/bin/luac luac.o liblua.a -L /gaston/lib -l gastonc

WORKDIR /output

CMD ["/bin/sh", "-c", "echo 'Lua ARM64 Linux binaries ready at /output' && ls -lh /output/bin/"]
