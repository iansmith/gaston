/* stdint.h — gaston fixed-width integer types */
#ifndef GASTON_STDINT_H
#define GASTON_STDINT_H

typedef long          int64_t;
typedef unsigned long uint64_t;
typedef int           int32_t;
typedef unsigned int  uint32_t;
typedef long          intmax_t;
typedef unsigned long uintmax_t;
typedef long          ssize_t;
typedef long          size_t;

#define INT64_MAX  9223372036854775807
#define UINT64_MAX 18446744073709551615
#define INT32_MAX  2147483647
#define SIZE_MAX   18446744073709551615

#endif /* GASTON_STDINT_H */
