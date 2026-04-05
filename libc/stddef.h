/* stddef.h — gaston common type definitions */
#ifndef GASTON_STDDEF_H
#define GASTON_STDDEF_H

typedef long            size_t;
typedef long            ptrdiff_t;
typedef long            intptr_t;
typedef unsigned int    wchar_t;
#define NULL 0
#define offsetof(type, member) __builtin_offsetof(type, member)

#endif /* GASTON_STDDEF_H */
