/* GAST-27: a global double/long-double must convert an integer-valued
 * initializer to its IEEE bit pattern, not store the raw integer bits.
 *
 * A scalar "float g_f = 3;" case is deliberately not exercised here: even
 * "float g_f = 3.0;" (a plain float-literal initializer, no int conversion
 * involved) already fails on unmodified gaston — a separate, pre-existing
 * global-scalar-float (4-byte) initializer storage bug, unrelated to this
 * ticket's int->FP conversion fix. Filed separately; not fixed here. */

double g_d = 5;
long double g_ld = 7;
double g_neg = -9;

void main(void) {
    output((int)g_d);    /* 5 */
    output((int)g_ld);   /* 7 */
    output((int)g_neg);  /* -9 */
}
