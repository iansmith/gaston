/*
 * SPDX-License-Identifier: BSD-3-Clause
 *
 * Copyright © 2019 Keith Packard
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 *
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 *
 * 2. Redistributions in binary form must reproduce the above
 *    copyright notice, this list of conditions and the following
 *    disclaimer in the documentation and/or other materials provided
 *    with the distribution.
 *
 * 3. Neither the name of the copyright holder nor the names of its
 *    contributors may be used to endorse or promote products derived
 *    from this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS
 * FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE
 * COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
 * SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
 * STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED
 * OF THE POSSIBILITY OF SUCH DAMAGE.
 */

#include "stdio_private.h"
#include <stdlib.h>
#include <unistd.h>

static char write_buf[BUFSIZ];
static char read_buf[BUFSIZ];

static struct __file_bufio __stdin;
static struct __file_bufio __stdout;

/* These pointers start NULL and are set by __posix_stdio_init(). */
FILE *__posix_stdin;
FILE *__posix_stdout;
FILE *__posix_stderr;
FILE *stdin;
FILE *stdout;
FILE *stderr;

/*
 * Runtime initializer — called from _start before main().
 * Sets up the __stdin/__stdout bufio structs and wires the
 * public FILE* pointers to them.
 */
void __posix_stdio_init(void)
{
    /* __stdin: flags = __SRD | __SCLOSE | __SEXT | __SBUF */
    __stdin.xfile.cfile.file.flags = (0x0001 | 0x0040 | 0x0020 | 0x0010);
    __stdin.xfile.cfile.file.put   = __bufio_put;
    __stdin.xfile.cfile.file.get   = __bufio_get;
    __stdin.xfile.cfile.file.flush = __bufio_flush;
    __stdin.xfile.cfile.close      = __bufio_close;
    __stdin.xfile.seek             = __bufio_seek;
    __stdin.xfile.setvbuf          = __bufio_setvbuf;
    __stdin.fd    = 0;
    __stdin.dir   = 0;
    __stdin.bflags = 0;
    __stdin.pos   = 0;
    __stdin.buf   = read_buf;
    __stdin.size  = BUFSIZ;
    __stdin.len   = 0;
    __stdin.off   = 0;
    __stdin.read  = (void *)read;
    __stdin.write = (void *)write;
    __stdin.lseek = lseek;
    __stdin.close = close;

    /* __stdout: flags = __SWR | __SCLOSE | __SEXT | __SBUF, bflags = __BLBF */
    __stdout.xfile.cfile.file.flags = (0x0002 | 0x0040 | 0x0020 | 0x0010);
    __stdout.xfile.cfile.file.put   = __bufio_put;
    __stdout.xfile.cfile.file.get   = __bufio_get;
    __stdout.xfile.cfile.file.flush = __bufio_flush;
    __stdout.xfile.cfile.close      = __bufio_close;
    __stdout.xfile.seek             = __bufio_seek;
    __stdout.xfile.setvbuf          = __bufio_setvbuf;
    __stdout.fd    = 1;
    __stdout.dir   = 0;
    __stdout.bflags = 0x0002;
    __stdout.pos   = 0;
    __stdout.buf   = write_buf;
    __stdout.size  = BUFSIZ;
    __stdout.len   = 0;
    __stdout.off   = 0;
    __stdout.read  = (void *)read;
    __stdout.write = (void *)write;
    __stdout.lseek = lseek;
    __stdout.close = close;

    /* Wire up the public FILE* pointers */
    __posix_stdin  = &__stdin.xfile.cfile.file;
    __posix_stdout = &__stdout.xfile.cfile.file;
    __posix_stderr = &__stdout.xfile.cfile.file;
    stdin  = &__stdin.xfile.cfile.file;
    stdout = &__stdout.xfile.cfile.file;
    stderr = &__stdout.xfile.cfile.file;
}
