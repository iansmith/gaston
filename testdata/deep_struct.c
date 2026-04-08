/* deep_struct.cm — 5-level nested struct.
   Stresses: SizeBytes recursion, frame allocation, chained dot codegen.

   LP64 layout (int=4; struct fields always 8-byte aligned in gaston):
     L5 { x }                       : x@0(4), size=4
     L4 { a4; struct L5 leaf; }     : a4@0(4), leaf@8(4), size=16
     L3 { a3; struct L4 lvl4; }     : a3@0(4), lvl4@8(16), size=24
     L2 { a2; struct L3 lvl3; }     : a2@0(4), lvl3@8(24), size=32
     L1 { a1; struct L2 lvl2; }     : a1@0(4), lvl2@8(32), size=40

   Expected output: 1 2 3 4 5 4 16 24 32 40 */

struct L5 { int x; };
struct L4 { int a4; struct L5 leaf; };
struct L3 { int a3; struct L4 lvl4; };
struct L2 { int a2; struct L3 lvl3; };
struct L1 { int a1; struct L2 lvl2; };

void main(void) {
    struct L1 r;
    r.a1 = 1;
    r.lvl2.a2 = 2;
    r.lvl2.lvl3.a3 = 3;
    r.lvl2.lvl3.lvl4.a4 = 4;
    r.lvl2.lvl3.lvl4.leaf.x = 5;

    output(r.a1);                        /* 1  — L1.a1  at frame offset 0 */
    output(r.lvl2.a2);                   /* 2  — L2.a2  at frame offset 8 */
    output(r.lvl2.lvl3.a3);              /* 3  — L3.a3  at frame offset 16 */
    output(r.lvl2.lvl3.lvl4.a4);        /* 4  — L4.a4  at frame offset 24 */
    output(r.lvl2.lvl3.lvl4.leaf.x);    /* 5  — L5.x   at frame offset 32 */

    output(sizeof(struct L5));  /* 4  */
    output(sizeof(struct L4));  /* 16 */
    output(sizeof(struct L3));  /* 24 */
    output(sizeof(struct L2));  /* 32 */
    output(sizeof(struct L1));  /* 40 */
}
