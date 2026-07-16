/* Tentative definitions (C11 6.9.2p2): file-scope objects without initializers.
 * gaston must give these real storage (BSS) so their addresses resolve at link
 * time. Exercises scalar, pointer, array, and 16-byte-aligned tentative defs. */

long *g;            /* tentative pointer */
long x;             /* tentative scalar */
int arr[4];         /* tentative array */
double dv;          /* tentative double (8-byte alignment) */
long double ldv;    /* tentative long double (16-byte alignment) */

void main(void) {
    x = 5;
    g = &x;
    arr[2] = 7;
    dv = 2.0;
    ldv = 3.0;
    output((int)*g);                         /* 5 */
    output(arr[2]);                          /* 7 */
    output(arr[0]);                          /* 0 — BSS must be zeroed */
    output((int)dv);                         /* 2 */
    output((int)ldv);                        /* 3 */
    output((int)((long)&ldv & 15));          /* 0 — 16-byte aligned */
}
