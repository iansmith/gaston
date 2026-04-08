/* float_basic: literal assignment, basic ops, int conversion */
int main(void) {
    double x;
    int n;
    x = 1.5;
    n = x;
    output(n);         /* 1 */
    x = x * 2.0;
    n = x;
    output(n);         /* 3 */
    x = 1.5;
    x = x + 0.5;
    n = x;
    output(n);         /* 2 */
    x = 1.5;
    x = x - 0.5;
    n = x;
    output(n);         /* 1 */
    return 0;
}
