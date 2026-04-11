/* alignas_enforce: _Alignas(N) must cause the variable's address to be a
   multiple of N.  The 'separator' variable between two aligned ones ensures
   the second alignment cannot be satisfied accidentally by stack convention. */
void main(void) {
    /* Two _Alignas(16) locals with an unaligned byte between them forces
       the compiler to actually insert padding for the second one. */
    _Alignas(16) char a;
    char separator;       /* one byte: likely breaks natural 16-alignment */
    _Alignas(16) char b;

    output((int)((long)&a % 16));  /* 0 — a is 16-byte aligned */
    output((int)((long)&b % 16));  /* 0 — b is also 16-byte aligned */

    /* 8-byte alignment on a 4-byte type */
    _Alignas(8) int x;
    output((int)((long)&x % 8));   /* 0 */

    /* struct: _Alignas on a member widens the struct's alignment and size */
    struct Padded {
        char  a;           /* offset 0, size 1 */
        _Alignas(8) int b; /* offset 8 (7 bytes of padding inserted) */
    };
    output((int)sizeof(struct Padded));  /* 16 (8-byte align rounds up total) */
    output((int)_Alignof(struct Padded)); /* 8 */

    struct Padded p;
    output((int)((long)&p.b % 8));  /* 0 — field itself is 8-byte aligned */
}
