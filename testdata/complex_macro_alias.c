/* GAST-25 review follow-up: the bare "complex" spelling (the C99 7.3.1p1
 * macro alias for "_Complex", defined by <complex.h>) must degrade the
 * same way as "_Complex" itself. This exercises gaston's own builtin
 * <complex.h> stub (preproc.go) — the fallback used when no real libc
 * header is on the include path — which must define
 * "#define complex _Complex" for the macro to ever reach the lexer. */

#include <complex.h>

complex double identity_d(complex double z) {
    return z;
}

void main(void) {
    complex double a = 2;
    output((int)identity_d(a));      /* 2 */
}
