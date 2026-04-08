/* union_float_chain.cm — union with double inside a nested struct, accessed
   via a multi-level pointer chain.
   Byzantine test combining items 2+3+9:
     - union Value { int i; double d } in a struct Tagged
     - struct Container { struct Tagged t; int extra; }
     - access via Container* → .t.v.d  (mixed chained access with FP union field)
     - verifies IRFFieldStore/IRFFieldLoad work through 3-level . chains
     - also verifies int alias (i) and double alias (d) in the same union

   Layout:
     union Value: i@0(8), d@0(8), size=8, align=8
     struct Tagged: kind@0(8), v@8(8), size=16, align=8
     struct Container: t@0(16), extra@16(8), size=24, align=8

   Expected: 42 3.140000 42 24 */

union Value  { int i; double d; };
struct Tagged { int kind; union Value v; };
struct Container { struct Tagged t; int extra; };

void set_int(struct Container *c, int val) {
    c->t.kind = 0;
    c->t.v.i = val;
}

void set_double(struct Container *c, double val) {
    c->t.kind = 1;
    c->t.v.d = val;
}

void main(void) {
    struct Container c;
    c.extra = 99;

    set_int(&c, 42);
    output(c.t.v.i);          /* 42 — int via 3-level dot chain */

    set_double(&c, 3.14);
    print_double(c.t.v.d);    /* 3.140000 — double via 3-level dot chain */

    /* Alias: after set_int, int union field == value stored */
    set_int(&c, 42);
    output(c.t.v.i);          /* 42 again */

    output(sizeof(struct Container));  /* 24 */
}
