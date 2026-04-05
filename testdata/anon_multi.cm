/* anon_multi.cm — two anonymous structs in one struct, plus pointer access.
   Exercises: multiple anonymous members, FindFieldDeep traversal order,
   -> field access through a pointer to a struct with anonymous members.

   LP64 layout (gaston: TypeStruct align=8):
     struct Vec4 { struct{int x;int y;}; struct{int z;int w;}; }
       __anon_0 @  0  (anon struct: x@0 y@4, size=8)
         x      @  0  (anon_base=0 + x_offset=0)
         y      @  4  (anon_base=0 + y_offset=4)
       __anon_1 @  8  (anon struct: z@0 w@4, size=8, already 8-aligned)
         z      @  8  (anon_base=8 + z_offset=0)
         w      @ 12  (anon_base=8 + w_offset=4)
     rawEnd=16, maxAlign=8 → sizeof=16

   Expected: 10 20 30 40 10 30 16 */

struct Vec4 {
    struct { int x; int y; };
    struct { int z; int w; };
};

int main(void) {
    struct Vec4 v;
    struct Vec4 *p;

    v.x = 10;
    v.y = 20;
    v.z = 30;
    v.w = 40;

    output(v.x);                /* 10 */
    output(v.y);                /* 20 */
    output(v.z);                /* 30 */
    output(v.w);                /* 40 */

    p = &v;
    output(p->x);               /* 10 — -> through anonymous member */
    output(p->z);               /* 30 — -> through second anonymous member */

    output(sizeof(struct Vec4)); /* 16 */
    return 0;
}
