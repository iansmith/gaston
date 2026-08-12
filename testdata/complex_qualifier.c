/* _Complex is a no-panic qualifier (GAST-25): the lexer must accept it
 * (silently dropping to the base real type, same as __thread/_Noreturn)
 * so declarations using it parse instead of derailing the grammar. No
 * complex arithmetic is implemented — every _Complex-typed value here is
 * just its underlying real type end to end.
 *
 * Float/long-double calls below use decimal-point literals (3.0, not 3):
 * passing a bare int literal to a float/long-double parameter hits an
 * unrelated, pre-existing gaston bug (int->float argument conversion at
 * call sites) — not in scope here, avoided rather than masked. */

double _Complex identity_d(double _Complex z) {
    return z;
}

float _Complex identity_f(float _Complex z) {
    return z;
}

long double _Complex identity_ld(long double _Complex z) {
    return z;
}

double _Complex g_dc;
_Complex double g_dc2;

void main(void) {
    double _Complex a = 2;
    output((int)identity_d(a));      /* 2 */
    output((int)identity_f(3.0));    /* 3 */
    output((int)identity_ld(4.0));   /* 4 */
    g_dc = 5;
    output((int)g_dc);               /* 5 */
    g_dc2 = 6;
    output((int)g_dc2);              /* 6 */
}
