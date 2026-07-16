/* Initialized 16-byte scalar globals: the .data slot must be 16 bytes with
 * the high word sign-extended, and a following global must not be
 * overlapped by the 128-bit access. */

__int128 g = -5;
long guard = 77;

void main(void) {
    output((int)(g < 0));        /* 1 — high word carries the sign */
    output((int)(long)g);        /* -5 */
    g = g + 1;
    output((int)(long)g);        /* -4 */
    output((int)guard);          /* 77 — neighbor intact */
}
