void main(void) {
    int n = 5;
    long *p = alloca(n * sizeof(long));
    p[0] = 10;
    p[1] = 20;
    p[2] = 30;
    output(p[0]);       /* 10 */
    output(p[2]);       /* 30 */
    output(p[1] + p[2]); /* 50 */
}
