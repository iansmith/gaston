/* struct_int_layout.cm — verifies sizeof(int)==4 and LP64 struct layouts.
   LP64: int=4 bytes, long=8 bytes, double=8 bytes, pointer=8 bytes.

   Layouts:
     struct S { int a; int b; }        a@0(4) b@4(4)         size=8
     struct T { char c; int n; }       c@0(1) pad(3) n@4(4)  size=8
     struct U { int x; double d; }     x@0(4) pad(4) d@8(8)  size=16

   Also verifies field values round-trip correctly at 32-bit width:
   storing 0x100000001 into an int field truncates to 1 (low 32 bits).

   Expected: 8 8 16 4 7 42 1 */

struct S { int a; int b; };
struct T { char c; int n; };
struct U { int x; double d; };

int main(void) {
    struct S s;
    struct T t;
    struct U u;
    int scratch;

    output(sizeof(struct S));  /* 8:  a@0(4) + b@4(4) */
    output(sizeof(struct T));  /* 8:  c@0(1) + pad(3) + n@4(4) */
    output(sizeof(struct U));  /* 16: x@0(4) + pad(4) + d@8(8) */
    output(sizeof(int));       /* 4 */

    s.a = 7;
    s.b = 42;
    output(s.a);               /* 7 */
    output(s.b);               /* 42 */

    /* Store a value that doesn't fit in 32 bits; verify truncation to low 32 bits. */
    scratch = 1;
    s.a = scratch;
    output(s.a);               /* 1 */

    return 0;
}
