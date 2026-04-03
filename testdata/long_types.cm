/* long and long long — both map to 64-bit int on ARM64.
   Tests that all keyword forms parse and function identically to int. */
int main(void) {
    long a;
    long long b;
    unsigned long c;
    unsigned long long d;

    a = 1000000000;
    b = 2000000000;
    c = 3000000000;
    d = 4000000000;

    output(a);            /* 1000000000 */
    output(b);            /* 2000000000 */
    output(c);            /* 3000000000 */
    output(d);            /* 4000000000 */
    output(a + b);        /* 3000000000 */

    /* long long function parameter and return */
    return 0;
}
