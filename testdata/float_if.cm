/* float_if: FP comparisons in if/while control flow */
int main(void) {
    double x;
    int n;
    x = 2.5;
    if (x > 2.0) {
        output(1);
    } else {
        output(0);
    }
    if (x < 2.0) {
        output(1);
    } else {
        output(0);
    }
    if (x == 2.5) {
        output(1);
    } else {
        output(0);
    }
    /* while controlled by FP comparison */
    x = 0.0;
    while (x < 3.0) {
        x = x + 1.0;
    }
    n = x;
    output(n);   /* 3 */
    return 0;
}
