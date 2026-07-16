/* Tentative storage must not overlap the heap: the linker computes the sbrk
 * break pointer from BSS extent — COMMON allocation must be inside that
 * extent, or malloc'd memory overlays tentative globals. Also verifies a
 * large (1 MB) COMMON allocates fully (far-end write). */

long tarr[8];
long big[131072];

void main(void) {
    int i;
    char *p;

    tarr[7] = 11;
    big[131071] = 1;

    p = malloc(64);
    for (i = 0; i < 64; i++) {
        p[i] = 0x5A;
    }

    output((int)tarr[7]);     /* 11 — survives malloc writes */
    output((int)tarr[0]);     /* 0  — still zero */
    output((int)big[131071]); /* 1  — far end of 1MB COMMON is real storage */
}
