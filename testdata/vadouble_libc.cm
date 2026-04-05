/* vadouble_libc.cm — test va_arg with double via libgastonc (object mode) */
#include <stdio.h>

long read_double_as_long(int n, ...) {
    long* ap;
    double x;
    long r;
    ap = __va_start();
    x = va_arg(ap, double);
    r = x;
    return r;
}

int main(void) {
    long a;
    long b;
    long c;
    a = read_double_as_long(1, 3.14);
    b = read_double_as_long(1, 7.9);
    c = read_double_as_long(1, 100.5);
    printf("%d %d %d\n", a, b, c);
    return 0;
}
