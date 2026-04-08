void main(void) {
    int x = 42;
    typeof(x) y = x + 1;     /* typeof(int var) -> int */
    double d = 3.14;
    typeof(d) e = d * 2.0;   /* typeof(double var) -> double */
    typeof(int) z = 100;     /* typeof(type) -> int */
    typeof(x + y) sum = x + y; /* typeof(int expr) -> int */
    output(y);                /* 43 */
    print_double(e);          /* 6.280000 */
    output(z);               /* 100 */
    output(sum);              /* 85 */
}
