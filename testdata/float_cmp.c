/* float_cmp: all six comparison operators */
int main(void) {
    double a;
    double b;
    a = 1.5;
    b = 2.5;
    output(a < b);   /* 1 */
    output(a <= b);  /* 1 */
    output(a > b);   /* 0 */
    output(a >= b);  /* 0 */
    output(a == b);  /* 0 */
    output(a != b);  /* 1 */
    output(a == a);  /* 1 */
    output(a != a);  /* 0 */
    return 0;
}
