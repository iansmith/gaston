/* Minimal stdio.h stub for gaston libm compilation.
   The libm sources that include stdio.h do not actually use FILE or I/O
   functions, so we provide only the type declarations needed to satisfy
   the include without function-pointer struct fields that gaston cannot parse. */
#ifndef _STDIO_H_
#define _STDIO_H_ 1

#include <stddef.h>

/* Opaque FILE type — libm code only needs the type name, not the internals. */
typedef struct __file FILE;

#define EOF (-1)

extern int printf(const char *fmt, ...);
extern int fprintf(FILE *stream, const char *fmt, ...);
extern int sprintf(char *buf, const char *fmt, ...);
extern int snprintf(char *buf, size_t n, const char *fmt, ...);
extern int scanf(const char *fmt, ...);
extern int sscanf(const char *buf, const char *fmt, ...);
extern int fscanf(FILE *stream, const char *fmt, ...);
extern int puts(const char *s);
extern int putchar(int c);

extern FILE *stdin;
extern FILE *stdout;
extern FILE *stderr;

#endif /* _STDIO_H_ */
