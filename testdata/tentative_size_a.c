/* Cross-TU tentative size mismatch, TU A: `buf` is long[1] here but long[4]
 * in TU B — the linker must allocate the MAXIMUM size (COMMON merge rule).
 * `disjoint` proves guard does not overlap buf's full 32 bytes. */

long buf[1];

void fill(void);
long get3(void);
int disjoint(void);

void main(void) {
    fill();
    output((int)buf[0]);   /* 5 */
    output((int)get3());   /* 7 — write to buf[3] survives: full size allocated */
    output(disjoint());    /* 1 — guard lies outside buf's 32 bytes */
}
