void main(void) {
    long result;
    int ovf;

    /* no overflow */
    ovf = __builtin_add_overflow(100L, 200L, &result);
    output(result);  /* 300 */
    output(ovf);     /* 0 */

    /* overflow: LLONG_MAX + 1 */
    ovf = __builtin_add_overflow(9223372036854775807L, 1L, &result);
    output(ovf);     /* 1 */

    /* no overflow: negative numbers */
    ovf = __builtin_add_overflow(-100L, 50L, &result);
    output(result);  /* -50 */
    output(ovf);     /* 0 */
}
