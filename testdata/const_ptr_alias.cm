/* const_ptr_alias.cm — const* aliasing: same memory reachable via
   a const pointer (read-only view) and a regular pointer (writable).
   Stresses: IsConstTarget flag in symEntry, const* assignment from int*,
   re-pointing const* to a different variable, const* pointer arithmetic.

   Expected: 42 99 99 100 10 30 */

void main(void) {
    int x;
    int y;
    const int *ro;
    int *rw;
    int arr[5];
    int *wp;
    const int *cp;

    x = 42;
    ro = &x;
    output(*ro);    /* 42 — initial read through const* */

    rw = &x;
    *rw = 99;       /* modify x through the writable alias */
    output(*ro);    /* 99 — const* sees the change */
    output(x);      /* 99 — direct read confirms */

    ro = &y;        /* re-seat const* to y — allowed */
    y = 100;
    output(*ro);    /* 100 */

    /* const* into an array via pointer arithmetic */
    wp = &arr;
    *wp = 10;       /* arr[0] = 10 */
    wp = wp + 2;
    *wp = 30;       /* arr[2] = 30 */
    cp = &arr;
    output(*cp);    /* 10 — arr[0] via const* */
    cp = cp + 2;
    output(*cp);    /* 30 — arr[2] via const* arithmetic */
}
