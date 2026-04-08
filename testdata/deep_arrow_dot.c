/* deep_arrow_dot.cm — 3-level mixed -> . . field access chains.
   Byzantine test for items 2+3: stresses the irgen fieldBasePtr fix for
   -> followed by nested . chains at depth 3, including a double field
   (exercising IRFFieldLoad/IRFFieldStore through the full chain).

   Layout:
     struct Leaf   { int x; double d; }        x@0(8) d@8(8)  size=16
     struct Branch { struct Leaf leaf; int y; } leaf@0(16) y@16(8)  size=24
     struct Root   { struct Branch branch; int z; } branch@0(24) z@24(8)  size=32

   Writes via pointer (populate), reads back via direct dot access in main.
   Access chain r->branch.leaf.x  = -> . .  (3-level mixed)
                r->branch.leaf.d  = -> . .  (3-level, FP field via IRFFieldLoad)
                r->branch.y       = -> .
                r->z              = ->

   Expected: 42 1.500000 99 7 16 24 32 */

struct Leaf   { int x; double d; };
struct Branch { struct Leaf leaf; int y; };
struct Root   { struct Branch branch; int z; };

void populate(struct Root *r) {
    r->branch.leaf.x = 42;
    r->branch.leaf.d = 1.5;
    r->branch.y = 99;
    r->z = 7;
}

void main(void) {
    struct Root r;
    populate(&r);
    output(r.branch.leaf.x);        /* 42 */
    print_double(r.branch.leaf.d);  /* 1.500000 */
    output(r.branch.y);             /* 99 */
    output(r.z);                    /* 7 */
    output(sizeof(struct Leaf));    /* 16 */
    output(sizeof(struct Branch));  /* 24 */
    output(sizeof(struct Root));    /* 32 */
}
