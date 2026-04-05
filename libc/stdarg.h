/* stdarg.h — gaston variadic argument support */
#ifndef GASTON_STDARG_H
#define GASTON_STDARG_H

/* va_list is a pointer into the caller's register-save area (long*). */
typedef long* va_list;

/* va_start initialises ap to point at the first variadic argument slot.
   The __va_start() built-in is resolved by the compiler to the save-area
   address computed from the current frame pointer. */
#define va_start(ap, last)  ap = __va_start()

/* va_arg is a compiler built-in keyword: va_arg(ap, T) reads *ap as T
   and advances ap by one slot.  No macro expansion needed. */

/* va_end is a no-op in gaston (no cleanup required). */
#define va_end(ap)

#endif /* GASTON_STDARG_H */
