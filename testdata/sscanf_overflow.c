/* sscanf_overflow.cm — test P2-C signed integer overflow clamping */
#include <stdio.h>

int main(void) {
    long x;
    int n;

    /* overflow: value > LLONG_MAX → clamped to LLONG_MAX */
    n = sscanf("99999999999999999999", "%d", &x);
    printf("n=%d v=%ld\n", n, x);

    /* overflow with negative: value < LLONG_MIN → clamped to LLONG_MIN */
    n = sscanf("-99999999999999999999", "%d", &x);
    printf("n=%d v=%ld\n", n, x);

    /* exact LLONG_MAX — should parse fine */
    n = sscanf("9223372036854775807", "%d", &x);
    printf("n=%d v=%ld\n", n, x);

    /* LLONG_MAX + 1 — one over, clamp to LLONG_MAX */
    n = sscanf("9223372036854775808", "%d", &x);
    printf("n=%d v=%ld\n", n, x);

    return 0;
}
