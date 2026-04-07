/* Copyright (c) 2016 Corinna Vinschen <corinna@vinschen.de> */
/* Definition of opaque POSIX-1.2008 type locale_t for userspace. */

#ifndef _SYS__LOCALE_H
#define _SYS__LOCALE_H

/* locale_t is a pointer to the internal locale struct (matches picolibc internals). */
typedef struct __locale_t *locale_t;

#endif /* _SYS__LOCALE_H */
