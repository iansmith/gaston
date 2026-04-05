/* vadouble_basic.cm — test va_arg with double */

long read_double_as_long(int n, ...) {
    long* ap;
    double x;
    long r;
    ap = __va_start();
    x = va_arg(ap, double);
    r = x;
    return r;
}

void main(void) {
    output(read_double_as_long(1, 3.14));
    output(read_double_as_long(1, 7.9));
    output(read_double_as_long(1, 100.5));
}
