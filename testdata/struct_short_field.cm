/* struct_short_field.cm — struct with a short field followed by an int field.
   LP64 layout: short@0 (2 bytes), pad 2, int@4 (4 bytes, align 4), size=8. */
struct S2 { short s; int i; };

void main(void) {
    struct S2 m;
    struct S2 n;
    m.s = 1000;
    m.i = 42;
    output(m.s);               /* 1000 — short field at offset 0 */
    output(m.i);               /* 42   — int field at offset 4 */
    output(sizeof(struct S2)); /* 8    — short(2)+pad(2)+int(4) */
    /* A second struct verifies the first struct did not corrupt stack */
    n.s = 32767;
    n.i = 9999;
    output(n.s);               /* 32767 — max positive short */
    output(n.i);               /* 9999 */
}
