#ifndef _FLOAT_H
#define _FLOAT_H

/* IEEE 754 single precision (float) */
#define FLT_RADIX       2
#define FLT_MANT_DIG    24
#define FLT_DIG         6
#define FLT_MIN_EXP     (-125)
#define FLT_MAX_EXP     128
#define FLT_MIN_10_EXP  (-37)
#define FLT_MAX_10_EXP  38
#define FLT_MAX         3.40282346638528860e+38f
#define FLT_MIN         1.17549435082228751e-38f
#define FLT_EPSILON     1.19209289550781250e-07f

/* IEEE 754 double precision (double) */
#define DBL_MANT_DIG    53
#define DBL_DIG         15
#define DBL_MIN_EXP     (-1021)
#define DBL_MAX_EXP     1024
#define DBL_MIN_10_EXP  (-307)
#define DBL_MAX_10_EXP  308
#define DBL_MAX         1.79769313486231571e+308
#define DBL_MIN         2.22507385850720138e-308
#define DBL_EPSILON     2.22044604925031308e-16

/* long double == double on AArch64 */
#define LDBL_MANT_DIG   DBL_MANT_DIG
#define LDBL_DIG        DBL_DIG
#define LDBL_MIN_EXP    DBL_MIN_EXP
#define LDBL_MAX_EXP    DBL_MAX_EXP
#define LDBL_MIN_10_EXP DBL_MIN_10_EXP
#define LDBL_MAX_10_EXP DBL_MAX_10_EXP
#define LDBL_MAX        DBL_MAX
#define LDBL_MIN        DBL_MIN
#define LDBL_EPSILON    DBL_EPSILON

#endif /* _FLOAT_H */
