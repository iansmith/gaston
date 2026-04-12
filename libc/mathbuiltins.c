/* mathbuiltins.cm — compiler builtins and math helpers needed by tinystdio.
 *
 * These implement GCC/clang __builtin_* and C99 math functions that
 * tinystdio's dtoa_engine and vfprintf expect.  They use IEEE 754
 * bit manipulation via unions.
 */

union __fp_bits { unsigned long u; double f; };
union __fpf_bits { unsigned int u; float f; };

/* ── signbit ─────────────────────────────────────────────────────────── */
/* Gaston's prelude defines __builtin_signbit* as macros; undef them here  */
/* so we can define the actual functions that back those macros.            */
#undef __builtin_signbit
#undef __builtin_signbitf
#undef __builtin_signbitl

int __builtin_signbit(double x) {
    union __fp_bits b;
    b.f = x;
    return (int)(b.u >> 63);
}

int __builtin_signbitf(float x) {
    /* promote to double for bit access since gaston doesn't have float unions */
    double d;
    d = x;
    union __fp_bits b;
    b.f = d;
    return (int)(b.u >> 63);
}

int __builtin_signbitl(double x) {
    return __builtin_signbit(x);
}

/* ── isnan ───────────────────────────────────────────────────────────── */

int __isnand(double x) {
    union __fp_bits b;
    b.f = x;
    /* NaN: exponent all 1s AND mantissa nonzero */
    unsigned long exp;
    unsigned long mant;
    exp = (b.u >> 52) & 0x7FF;
    mant = b.u & 0x000FFFFFFFFFFFFF;
    if ((exp == 0x7FF) & (mant != 0)) { return 1; }
    return 0;
}

int __isnanf(float x) {
    double d;
    d = x;
    return __isnand(d);
}

int __isnanl(double x) {
    return __isnand(x);
}

/* ── isinf ───────────────────────────────────────────────────────────── */

int __isinfd(double x) {
    union __fp_bits b;
    b.f = x;
    /* Inf: exponent all 1s AND mantissa zero */
    unsigned long exp;
    unsigned long mant;
    exp = (b.u >> 52) & 0x7FF;
    mant = b.u & 0x000FFFFFFFFFFFFF;
    if ((exp == 0x7FF) & (mant == 0)) {
        if (b.u >> 63) { return -1; }
        return 1;
    }
    return 0;
}

int __isinff(float x) {
    double d;
    d = x;
    return __isinfd(d);
}

int __isinfl(double x) {
    return __isinfd(x);
}

/* ── round ───────────────────────────────────────────────────────────── */

double round(double x) {
    /* Add 0.5 (or -0.5) and truncate to integer. */
    if (__isnand(x)) { return x; }
    if (__isinfd(x)) { return x; }
    if (x >= 0.0) {
        long i;
        i = (long)(x + 0.5);
        return (double)i;
    } else {
        long i;
        i = (long)(x - 0.5);
        return (double)i;
    }
}
