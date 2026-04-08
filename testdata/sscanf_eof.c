/* sscanf_eof.cm — test P2-D: EOF return when string exhausted before conversion */
#include <stdio.h>

int main(void) {
    long x;
    int n;

    /* empty string → EOF (-1) */
    n = sscanf("", "%d", &x);
    printf("n=%d\n", n);

    /* whitespace-only string, format has %d → no conversion, EOF */
    n = sscanf("   ", "%d", &x);
    printf("n=%d\n", n);

    /* successful conversion: should still return 1 */
    n = sscanf("42", "%d", &x);
    printf("n=%d v=%ld\n", n, x);

    /* literal mismatch (not EOF): return 0, not -1 */
    n = sscanf("abc", "xyz%d", &x);
    printf("n=%d\n", n);

    return 0;
}
