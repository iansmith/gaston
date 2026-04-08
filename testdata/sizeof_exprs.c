/* sizeof_exprs.cm — sizeof used in arithmetic expressions and as loop bound.
   Byzantine test for item 6: sizeof(local array) used as a computed value,
   as a divisor (element count), in comparisons, and indirectly via a function.
   Mixes items 6 + 8 (promotion: the division result stays as int).

   Note: gaston local int arrays use 8-byte frame slots per element, so
   sizeof(int arr[N]) == N*8 even though sizeof(int)==4.  Element count is
   sizeof(arr)/8, not sizeof(arr)/sizeof(int).

   Expected: 40 4 5 15 1 0 */

int sum_n(int *p, int n) {
    int s;
    int i;
    s = 0;
    i = 0;
    while (i < n) {
        s = s + p[i];
        i = i + 1;
    }
    return s;
}

void main(void) {
    int arr[5];
    int i;
    int n;
    i = 0;
    while (i < 5) {
        arr[i] = i + 1;   /* 1, 2, 3, 4, 5 */
        i = i + 1;
    }

    output(sizeof(arr));                       /* 40 = 5 * 8 (frame slots) */
    output(sizeof(int));                       /* 4 — LP64 C ABI */
    n = sizeof(arr) / 8;                       /* 40 / 8 = 5 (frame slot size) */
    output(n);                                 /* 5 */
    output(sum_n(&arr, n));                    /* 1+2+3+4+5 = 15 */
    output(sizeof(arr) == 40);                 /* 1 (true) */
    output(sizeof(arr) == sizeof(int));        /* 0 (false: 40 != 4) */
}
