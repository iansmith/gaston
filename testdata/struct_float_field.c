/* struct_float_field.cm — struct with a float field followed by an int field.
   LP64 layout: float@0 (4 bytes), int@4 (4 bytes), sizeof(struct FI)=8.
   Uses a local float variable as intermediary to avoid AddrFConst-to-integer-reg issue. */
float fval;
struct FI { float f; int i; };

void main(void) {
    struct FI m;
    fval = 3.5;
    m.f = fval;              /* copy float var into struct float field */
    m.i = 100;
    print_double(m.f);       /* 3.500000 — float field read back as FP value */
    output(m.i);             /* 100      — int field unaffected by float at offset 0 */
    output(sizeof(struct FI)); /* 8      — float(4)+int(4) */

    /* Overwrite the float field and verify int field is still intact */
    fval = 9.75;
    m.f = fval;
    print_double(m.f);       /* 9.750000 */
    output(m.i);             /* 100      — int field still 100 */
}
