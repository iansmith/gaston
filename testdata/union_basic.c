/* union_basic.cm — basic union: all fields share offset 0.
   LP64: sizeof(int)=4, so sizeof(union U) = max(4, 1) = 4. */
union U { int x; char c; };

void main(void) {
    union U v;
    v.x = 0x41424344;
    output(v.x);           /* 1094861636 = 0x41424344 */
    v.c = 65;              /* write 'A' (0x41) into the low byte */
    output(v.c);           /* 65 */
    output(sizeof(union U)); /* 4 */
}
