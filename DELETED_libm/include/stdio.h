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

extern FILE *fopen(const char *path, const char *mode);
extern int fclose(FILE *stream);
extern int fflush(FILE *stream);
extern int fileno(FILE *stream);
extern size_t fread(void *ptr, size_t size, size_t nmemb, FILE *stream);
extern size_t fwrite(const void *ptr, size_t size, size_t nmemb, FILE *stream);
extern int feof(FILE *stream);
extern int ferror(FILE *stream);
extern int fgetc(FILE *stream);
extern int fputc(int c, FILE *stream);
extern char *fgets(char *s, int n, FILE *stream);
extern int fputs(const char *s, FILE *stream);
extern int ungetc(int c, FILE *stream);
extern int remove(const char *path);
extern int rename(const char *oldpath, const char *newpath);
extern long ftell(FILE *stream);
extern int fseek(FILE *stream, long offset, int whence);
extern void rewind(FILE *stream);
extern void clearerr(FILE *stream);
extern int vfprintf(FILE *stream, const char *fmt, __builtin_va_list ap);
extern int vsnprintf(char *buf, size_t n, const char *fmt, __builtin_va_list ap);
extern int vprintf(const char *fmt, __builtin_va_list ap);
extern int vsprintf(char *buf, const char *fmt, __builtin_va_list ap);
extern void perror(const char *s);

#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2
#define BUFSIZ 1024

extern FILE *stdin;
extern FILE *stdout;
extern FILE *stderr;

#endif /* _STDIO_H_ */
