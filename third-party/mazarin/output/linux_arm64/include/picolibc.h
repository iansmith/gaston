/* Minimal picolibc.h stub for gaston compilation experiments */
#pragma once

#define _NEWLIB_VERSION "4.3.0"
#define __NEWLIB__      4
#define __NEWLIB_MINOR__ 3
#define __NEWLIB_PATCHLEVEL__ 0
#define __PICOLIBC__    2
#define __PICOLIBC_MINOR__ 1
#define __PICOLIBC_PATCHLEVEL__ 0
#define __PICOLIBC_VERSION__ "2.1.0"
#define __PICOLIBC_MINOR__ 1

/* Use global errno */
#define __GLOBAL_ERRNO 1

/* Math errno behaviour */
#define WANT_ERRNO 1
#define __MATH_ERRNO 1

/* Use IEEE libm */
/* #define __IEEE_LIBM */

/* No thread-local storage in our bare-metal target */
/* #define __THREAD_LOCAL_STORAGE */

/* Static atexit */
/* #define _ATEXIT_DYNAMIC_ALLOC */

/* No nano stdio */
/* #define __TINY_STDIO */

/* IO defaults */
#define __IO_DEFAULT 'f'
#define __OBSOLETE_MATH_FLOAT 0
#define __OBSOLETE_MATH_DOUBLE 0

/* No complex support for now */
/* #define __HAVE_COMPLEX */

/* Init array support */
#define __INIT_FINI_ARRAY 1

/* Single thread (no reentrant needed) */
#define __SINGLE_THREAD 1

#define __ELIX_LEVEL 0
