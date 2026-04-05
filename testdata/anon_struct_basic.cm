/* anon_struct_basic.cm — anonymous struct member: fields of the inline
   struct are directly accessible on the outer struct.

   LP64 layout (gaston: TypeStruct align=8):
     struct Outer { int a; struct{int b; int c;}; int d; }
       a        @  0  (int, 4)
       __anon_0 @  8  (anon struct: b@0 c@4, size=8, align=8 forces gap at 4-7)
         b      @  8  (absolute: anon_base=8 + b_offset=0)
         c      @ 12  (absolute: anon_base=8 + c_offset=4)
       d        @ 16  (int, 4)
     rawEnd=20, maxAlign=8 → sizeof=24

   Expected: 1 2 3 4 24 */

struct Outer {
    int a;
    struct {
        int b;
        int c;
    };
    int d;
};

int main(void) {
    struct Outer o;
    o.a = 1;
    o.b = 2;
    o.c = 3;
    o.d = 4;
    output(o.a);                   /* 1  */
    output(o.b);                   /* 2  */
    output(o.c);                   /* 3  */
    output(o.d);                   /* 4  */
    output(sizeof(struct Outer));  /* 24 */
    return 0;
}
