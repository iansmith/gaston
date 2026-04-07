/* stdio.h — gaston C standard I/O declarations */
#ifndef GASTON_STDIO_H
#define GASTON_STDIO_H

/* putchar: write one character to stdout; returns c */
extern int putchar(int c);

/* puts: write string s followed by a newline; returns 0 */
extern int puts(char* s);

/* printf: formatted output to stdout. Returns 0. */
extern int printf(char* fmt, ...);

/* sprintf: format into null-terminated buffer buf (no size limit).
   Returns number of characters written, not counting the null byte. */
extern int sprintf(char* buf, char* fmt, ...);

/* snprintf: format into buf writing at most n-1 chars plus a null byte.
   Returns number of characters written, not counting the null byte. */
extern int snprintf(char* buf, long n, char* fmt, ...);

/* fflush: flush buffered output for the given stream.
   Calls fdatasync on the underlying fd to flush the shepherd's line buffer.
   Returns 0 on success, -1 on error. */
extern int fflush(void* stream);

/* sscanf: scan formatted input from string str according to fmt.
   Supported conversions: %d (long decimal into long*), %s (word into char*),
   %c (single char into long*).  Returns number of items successfully read. */
extern int sscanf(char* str, char* fmt, ...);

#endif /* GASTON_STDIO_H */
