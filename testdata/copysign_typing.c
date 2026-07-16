/* __builtin_copysign must be typed as double-returning: assignment into a
 * double, casts, and arithmetic on the result must emit real FP conversions
 * (not pass raw IEEE bits through integer registers). */

void main(void) {
    double r;
    int n;

    r = __builtin_copysign(3.0, -1.0);
    n = (int)r;
    output(n);                                    /* -3 */
    output((int)(r + 1.0));                       /* -2 — result stays FP */
    output((int)__builtin_copysign(2.0, 1.0));    /* 2 — direct cast */

    r = __builtin_copysign(0.0, -1.0);            /* -0.0 */
    output((int)(r == 0.0));                      /* 1 — negative zero compares equal */
}
