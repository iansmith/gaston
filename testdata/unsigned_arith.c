/* unsigned arithmetic: add, sub, mul, compound assignments, unsigned long */
int main(void) {
    unsigned int a;
    unsigned int b;
    unsigned int c;
    unsigned long ul;

    a = 10;
    b = 3;

    output(a + b);   /* 13 */
    output(a - b);   /* 7  */
    output(a * b);   /* 30 */

    /* chain: result of unsigned binop is still unsigned */
    c = (a + b) / b;
    output(c);       /* 4 */

    /* compound assignments */
    a += 5;    output(a);   /* 15 */
    a -= 3;    output(a);   /* 12 */
    a *= 2;    output(a);   /* 24 */
    a /= 4;    output(a);   /* 6  */
    a %= 4;    output(a);   /* 2  */

    /* unsigned long */
    ul = 1000000000;
    ul += 1000000000;
    output(ul);   /* 2000000000 */

    return 0;
}
