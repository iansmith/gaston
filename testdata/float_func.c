/* float_func: double function parameters and return values */
double double_it(double x) {
    return x * 2.0;
}
double add_half(double x) {
    return x + 0.5;
}
int main(void) {
    double r;
    int n;
    r = double_it(3.0);
    n = r;
    output(n);   /* 6 */
    r = double_it(1.25);
    n = r;
    output(n);   /* 2  (2.5 → 2) */
    r = add_half(4.5);
    n = r;
    output(n);   /* 5 */
    r = add_half(1.5);
    r = double_it(r);
    n = r;
    output(n);   /* 4  (double_it(2.0) = 4.0) */
    return 0;
}
