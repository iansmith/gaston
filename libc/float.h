/* float.h — gaston floating-point limit constants */
#ifndef GASTON_FLOAT_H
#define GASTON_FLOAT_H

/* double */
#define DBL_MANT_DIG    53
#define DBL_DIG         15
#define DBL_MIN_EXP     (-1021)
#define DBL_MAX_EXP     1024
#define DBL_MIN_10_EXP  (-307)
#define DBL_MAX_10_EXP  308
#define DBL_MAX         1.7976931348623157e+308
#define DBL_MIN         2.2250738585072014e-308
#define DBL_EPSILON     2.2204460492503131e-16
#define DBL_DECIMAL_DIG 17

/* float */
#define FLT_MANT_DIG    24
#define FLT_DIG         6
#define FLT_MIN_EXP     (-125)
#define FLT_MAX_EXP     128
#define FLT_MIN_10_EXP  (-37)
#define FLT_MAX_10_EXP  38
#define FLT_MAX         3.4028235e+38
#define FLT_MIN         1.1754944e-38
#define FLT_EPSILON     1.1920929e-07
#define FLT_DECIMAL_DIG 9

/* long double == double on gaston (64-bit IEEE 754).
   LDBL_MANT_DIG == 53 is the signal picolibc/fdlibm use to detect this. */
#define LDBL_MANT_DIG    53
#define LDBL_DIG         15
#define LDBL_MIN_EXP     (-1021)
#define LDBL_MAX_EXP     1024
#define LDBL_MIN_10_EXP  (-307)
#define LDBL_MAX_10_EXP  308
#define LDBL_MAX         1.7976931348623157e+308
#define LDBL_MIN         2.2250738585072014e-308
#define LDBL_EPSILON     2.2204460492503131e-16
#define LDBL_DECIMAL_DIG 17

/* radix and rounding */
#define FLT_RADIX        2
#define FLT_ROUNDS       1
#define FLT_EVAL_METHOD  0
#define DECIMAL_DIG      17

#endif /* GASTON_FLOAT_H */
