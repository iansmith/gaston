/* float_arith: all four arithmetic operators with exact binary fractions */
int main(void) {
    double a;
    double b;
    double r;
    int n;
    a = 4.0;
    b = 1.5;
    r = a + b;
    n = r;
    output(n);   /* 5  (5.5 → 5) */
    r = a - b;
    n = r;
    output(n);   /* 2  (2.5 → 2) */
    r = a * b;
    n = r;
    output(n);   /* 6  (6.0) */
    a = 9.0;
    b = 3.0;
    r = a / b;
    n = r;
    output(n);   /* 3  (3.0) */
    a = 7.5;
    b = 2.5;
    r = a / b;
    n = r;
    output(n);   /* 3  (3.0) */
    return 0;
}
