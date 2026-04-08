/* const_ptr_write.cm — const* aliasing: same memory via const* and writable ptr.
   Byzantine test for item 9 (const*): multiple const* pointing to the same
   variable (via the writable pointer alias), const* re-seat, and const*
   passed to a function.  Exercises IsConstTarget through several code paths.

   Expected: 10 20 20 30 */

void peek(const int *p) {
    output(*p);
}

void main(void) {
    int x;
    int y;
    int *rw;
    const int *ro;

    x = 10;
    ro = &x;
    peek(ro);         /* 10 */

    rw = &x;
    *rw = 20;
    peek(ro);         /* 20 — const* sees change through writable alias */
    output(*ro);      /* 20 */

    ro = &y;          /* re-seat const* */
    y = 30;
    peek(ro);         /* 30 */
}
