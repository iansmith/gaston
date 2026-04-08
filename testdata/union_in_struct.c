/* union_in_struct.cm — struct containing a union, union containing a struct.
   Stresses: union field in struct grammar, SizeBytes for mixed nesting,
   aliasing semantics (all union fields share offset 0).

   LP64 layout:
     struct Point { int x; int y; }            : x@0(4) y@4(4), size=8
     union Coord  { struct Point xy; int raw; } : max(8, 4)=8, align=8, size=8
     struct Shape { int kind; union Coord pos; }: kind@0(4), pos@8(8), size=16
     union AnyShape{ struct Shape s; int tag; } : max(16, 4)=16, align=8, size=16

   Expected: 7 3 4 3 8 8 16 16 */

struct Point { int x; int y; };
union Coord { struct Point xy; int raw; };
struct Shape { int kind; union Coord pos; };
union AnyShape { struct Shape s; int tag; };

void main(void) {
    struct Shape sh;
    sh.kind = 7;
    sh.pos.xy.x = 3;
    sh.pos.xy.y = 4;
    output(sh.kind);        /* 7  — kind at offset 0 of Shape */
    output(sh.pos.xy.x);    /* 3  — x at offset 8+0+0 = 8 */
    output(sh.pos.xy.y);    /* 4  — y at offset 8+0+4 = 12 */
    output(sh.pos.raw);     /* 3  — union: raw aliases xy.x (both at offset 8 of Shape) */

    output(sizeof(struct Point));  /* 8  */
    output(sizeof(union Coord));   /* 8  */
    output(sizeof(struct Shape));  /* 16 */
    output(sizeof(union AnyShape));/* 16 */
}
