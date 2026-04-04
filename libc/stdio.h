/* stdio.h — gaston C standard I/O declarations */
#ifndef GASTON_STDIO_H
#define GASTON_STDIO_H

/* putchar: write one character to stdout; returns c */
extern int putchar(int c);

/* puts: write string s followed by a newline; returns 0 */
extern int puts(char* s);

/* printf: formatted output.
   Supported conversions: %d (long decimal), %s (string), %c (char), %% (literal %).
   Returns 0. */
extern int printf(char* fmt, ...);

/* sscanf — not yet implemented */

#endif /* GASTON_STDIO_H */
