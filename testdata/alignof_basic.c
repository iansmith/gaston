void main(void) {
    output(_Alignof(char));    /* 1 */
    output(_Alignof(int));     /* 4 */
    output(_Alignof(long));    /* 8 */
    output(_Alignof(double));  /* 8 */
    output(_Alignof(short));   /* 2 */
}
