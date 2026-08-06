/* See weak_uninit_a.c. */
extern int zed;

void main(void) {
    output(zed); /* 0 — zero-initialized weak .bss def, not a VA-0 crash */
}
