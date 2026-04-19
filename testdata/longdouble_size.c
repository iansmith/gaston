void main(void) {
    output((int)sizeof(long double));    /* 16 */
    output((int)_Alignof(long double));  /* 16 */
    long double x;
    x = 3.0;
    output((int)x);                      /* 3 */
}
