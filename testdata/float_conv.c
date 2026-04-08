/* float_conv: int<->double conversions via assignment */
int main(void) {
    double x;
    int n;
    /* double -> int truncation */
    x = 7.75;
    n = x;
    output(n);     /* 7 */
    /* int -> double -> arith -> int */
    n = 5;
    x = n;
    x = x * 3.0;
    n = x;
    output(n);     /* 15 */
    /* chain: int -> double -> arith -> int */
    n = 12;
    x = n;
    x = x / 4.0;
    n = x;
    output(n);     /* 3 */
    /* negative truncation toward zero */
    x = -2.9;
    n = x;
    output(n);     /* -2 */
    return 0;
}
