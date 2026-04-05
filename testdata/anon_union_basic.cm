/* anon_union_basic.cm — anonymous union inside a struct: all union fields
   share offset 0 within the union, so they alias each other.

   LP64 layout (gaston: TypeStruct align=8):
     struct Tagged { int tag; union { int ival; char cval; }; }
       tag      @  0  (int, 4)
       __anon_0 @  8  (anon union: {ival@0 sz=4, cval@0 sz=1}; union size=4, align→8)
         ival   @  8  (absolute: anon_base=8 + ival_offset=0)
         cval   @  8  (absolute: anon_base=8 + cval_offset=0)
     rawEnd=12, maxAlign=8 → sizeof=16

   Aliasing check:
     After t.ival = 1000 (0x000003E8):
       bytes at @8 (little-endian): E8 03 00 00
     After t.cval = 65 (0x41):
       byte  at @8 overwritten:     41 03 00 00
     t.ival reads 4 bytes → 0x00000341 = 833

   Expected: 7 1000 65 833 16 */

struct Tagged {
    int tag;
    union {
        int  ival;
        char cval;
    };
};

int main(void) {
    struct Tagged t;
    t.tag  = 7;
    t.ival = 1000;
    output(t.tag);                  /* 7    */
    output(t.ival);                 /* 1000 */
    t.cval = 65;                    /* overwrite low byte of ival */
    output(t.cval);                 /* 65   */
    output(t.ival);                 /* 833  (0x341: low byte now 65=0x41, next byte still 3) */
    output(sizeof(struct Tagged));  /* 16   */
    return 0;
}
