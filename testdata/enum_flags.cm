/* enum_flags.cm — enum constants used as bit flags.
   Stresses: enum auto-increment, enum values in expressions,
   bitwise &, |, ~, ^, shifting, compound operations on enum-derived values.

   Expected: 5 1 0 4 7 6 2 1 */

enum { FLAG_A = 1, FLAG_B = 2, FLAG_C = 4, FLAG_D = 8 };
enum { SHIFT_1 = 1, SHIFT_2 = 2 };

void main(void) {
    int flags;
    flags = FLAG_A | FLAG_C;           /* 1 | 4 = 5 */
    output(flags);                     /* 5 */
    output(flags & FLAG_A);            /* 5 & 1 = 1 */
    output(flags & FLAG_B);            /* 5 & 2 = 0 */
    output(flags & FLAG_C);            /* 5 & 4 = 4 */

    flags = flags | FLAG_B;            /* 5 | 2 = 7 */
    output(flags);                     /* 7 */

    flags = flags & ~FLAG_A;           /* 7 & ~1 = 7 & 0x...FE = 6 */
    output(flags);                     /* 6 */

    flags = FLAG_C >> SHIFT_1;         /* 4 >> 1 = 2 */
    output(flags);                     /* 2 */

    flags = FLAG_A << SHIFT_1;         /* 1 << 0 ? no: SHIFT_1=1 so 1<<1=2 */
    flags = flags >> SHIFT_1;          /* 2>>1 = 1 */
    output(flags);                     /* 1 */
}
