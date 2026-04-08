/* unsigned division and modulo.
   The critical case is dividing a value that is negative when signed.

   -1 as unsigned = 0xFFFFFFFFFFFFFFFF = 18446744073709551615
   Signed SDIV:   (-1) / 2 = 0
   Unsigned UDIV: UINT_MAX / 2 = 9223372036854775807 (> 0)          */
int main(void) {
    unsigned int a;
    unsigned int b;
    unsigned int big;
    unsigned int r;

    /* basic case */
    a = 100;
    b = 7;
    output(a / b);   /* 14 */
    output(a % b);   /* 2  */

    /* key correctness case: dividend is "negative" as signed */
    big = -1;        /* UINT_MAX = all bits set */
    b = 2;
    /* UDIV: UINT_MAX / 2 > 0  (correct) */
    /* SDIV:    -1    / 2 = 0  (wrong) */
    if (big / b > 0) { output(1); } else { output(0); }   /* expect 1 */

    /* unsigned mod: UINT_MAX % 2 = 1  (UINT_MAX is odd) */
    r = big % b;
    if (r == 1) { output(1); } else { output(0); }   /* expect 1 */

    return 0;
}
