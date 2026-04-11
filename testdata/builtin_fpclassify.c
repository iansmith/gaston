/* builtin_fpclassify: __builtin_isnan, __builtin_isinf, __builtin_copysign.
   volatile prevents the compiler from constant-folding the NaN/inf. */
void main(void) {
    volatile double zero = 0.0;
    double nan_val  = zero / zero;   /* IEEE 754: 0/0 = NaN */
    double inf_val  = 1.0 / zero;   /* IEEE 754: 1/0 = +Inf */
    double neg_inf  = -1.0 / zero;  /* -Inf */
    double finite_v = 1.5;

    /* __builtin_isnan: 1 for NaN, 0 for everything else */
    output(__builtin_isnan(nan_val));    /* 1 */
    output(__builtin_isnan(inf_val));    /* 0 — infinity is not NaN */
    output(__builtin_isnan(finite_v));   /* 0 */

    /* __builtin_isinf: 1 for +/-infinity, 0 for everything else */
    output(__builtin_isinf(inf_val));    /* 1 */
    output(__builtin_isinf(neg_inf));    /* 1 */
    output(__builtin_isinf(nan_val));    /* 0 — NaN is not infinity */
    output(__builtin_isinf(finite_v));   /* 0 */

    /* __builtin_copysign(mag, sign): magnitude of first arg, sign of second */
    int n;
    double r;
    r = __builtin_copysign(3.0, -1.0);
    n = (int)r;
    output(n);   /* -3 */

    r = __builtin_copysign(-5.0, 1.0);
    n = (int)r;
    output(n);   /* 5 */

    r = __builtin_copysign(4.0, 0.0);
    n = (int)r;
    output(n);   /* 4 — zero is positive */

    r = __builtin_copysign(4.0, neg_inf);
    n = (int)r;
    output(n);   /* -4 — negative infinity contributes negative sign */
}
