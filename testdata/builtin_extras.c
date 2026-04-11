/* Test newly added builtins */
void main(void) {
    /* __builtin_ffs: find first set bit */
    output(__builtin_ffs(0));    /* 0 */
    output(__builtin_ffs(1));    /* 1 */
    output(__builtin_ffs(4));    /* 3 */
    output(__builtin_ffs(6));    /* 2 */
    output(__builtin_ffsl(8));   /* 4 */

    /* __builtin_signbit: 1 if negative */
    volatile double pos = 1.0;
    volatile double neg = -1.0;
    volatile double zero_pos = 0.0;
    output(__builtin_signbit(pos));   /* 0 */
    output(__builtin_signbit(neg));   /* 1 */

    /* __builtin_isfinite: 1 if finite */
    volatile double zero = 0.0;
    double inf = 1.0 / zero;
    double nan = zero / zero;
    output(__builtin_isfinite(pos));  /* 1 */
    output(__builtin_isfinite(inf));  /* 0 */
    output(__builtin_isfinite(nan));  /* 0 */

    /* __builtin_isnormal */
    double subnorm = 5e-324;  /* smallest subnormal double */
    output(__builtin_isnormal(pos));      /* 1 */
    output(__builtin_isnormal(zero_pos)); /* 0 */
    output(__builtin_isnormal(subnorm));  /* 0 */

    /* __builtin_alloca test - just compile, not run */
}
