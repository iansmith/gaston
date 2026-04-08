/* printf.cm — gaston printf / puts / putchar backed by picolibc tinystdio.
 *
 * stdout, stderr, stdin are defined by picolibc's posixiob.c, which uses
 * real POSIX read/write syscalls.  This file just provides the variadic
 * wrappers that call picolibc's vfprintf engine.
 *
 * Provides: printf, fprintf, sprintf, snprintf, putchar, puts
 */

#include <stdarg.h>

/* ---- tinystdio FILE definition (must match picolibc's struct __file) ---- */

typedef unsigned short __ungetc_t;
typedef unsigned char uint8_t;
typedef unsigned long size_t;

struct __file {
    __ungetc_t unget;
    uint8_t flags;
    int (*put)(char, struct __file *);
    int (*get)(struct __file *);
    int (*flush)(struct __file *);
};

typedef struct __file FILE;

struct __file_str {
    struct __file file;
    char *pos;
    char *end;
    size_t size;
};

/* ---- Forward declarations for picolibc tinystdio functions -------------- */
extern int vfprintf(FILE *stream, const char *fmt, va_list ap);
extern int __file_str_put(char c, FILE *stream);

/* ---- stdout/stderr/stdin come from picolibc's posixiob.c ---------------- */
extern FILE *stdout;
extern FILE *stderr;
extern FILE *stdin;

/* ---- putchar / puts ----------------------------------------------------- */

int putchar(int c) {
    char buf[1];
    buf[0] = (char)c;
    write(1, buf, 1);
    return c;
}

int puts(char *s) {
    int i;
    i = 0;
    while (s[i] != 0) {
        i = i + 1;
    }
    write(1, s, i);
    write(1, "\n", 1);
    return 0;
}

/* ---- printf: format and print to stdout --------------------------------- */
int printf(char *fmt, ...) {
    va_list ap;
    ap = __va_start();
    return vfprintf(stdout, fmt, ap);
}

/* ---- fprintf: format and print to a stream ------------------------------ */
int fprintf(FILE *stream, char *fmt, ...) {
    va_list ap;
    ap = __va_start();
    return vfprintf(stream, fmt, ap);
}

/* ---- sprintf: format into null-terminated buffer (no size limit) -------- */
int sprintf(char *buf, char *fmt, ...) {
    struct __file_str f;
    va_list ap;

    f.file.unget = 0;
    f.file.flags = 2;
    f.file.put = __file_str_put;
    f.file.get = 0;
    f.file.flush = 0;
    f.pos = buf;
    f.end = buf + 2147483646;
    f.size = 0;

    ap = __va_start();
    int r;
    r = vfprintf(&f.file, fmt, ap);
    *f.pos = 0;
    return r;
}

/* ---- snprintf: format into buf writing at most n-1 chars plus null ------ */
int snprintf(char *buf, long n, char *fmt, ...) {
    struct __file_str f;
    va_list ap;
    int r;
    char *end_ptr;

    /* Per C99: snprintf(NULL, 0, ...) returns the would-be-written count.
     * We must always call vfprintf to get the count; the __file_str_put
     * function silently stops writing when pos == end. */
    if (n <= 0) {
        end_ptr = buf;  /* pos == end → nothing written */
    } else {
        end_ptr = buf + (n - 1);
    }

    f.file.unget = 0;
    f.file.flags = 2;
    f.file.put = __file_str_put;
    f.file.get = 0;
    f.file.flush = 0;
    f.pos = buf;
    f.end = end_ptr;
    f.size = 0;

    ap = __va_start();
    r = vfprintf(&f.file, fmt, ap);
    if (n > 0) { *f.pos = 0; }
    return r;
}
