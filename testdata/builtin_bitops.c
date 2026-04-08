void main(void) {
    /* __builtin_clz: count leading zeros in a 32-bit unsigned int */
    output(__builtin_clz(1));          /* 31: 0x00000001 has 31 leading zeros */
    output(__builtin_clz(0x80000000)); /* 0:  high bit set */
    output(__builtin_clz(0x00010000)); /* 15 */

    /* __builtin_ctz: count trailing zeros */
    output(__builtin_ctz(1));   /* 0:  bit 0 set */
    output(__builtin_ctz(8));   /* 3:  0b1000 */
    output(__builtin_ctz(16));  /* 4:  0b10000 */

    /* __builtin_popcount: count 1 bits */
    output(__builtin_popcount(0));          /* 0 */
    output(__builtin_popcount(1));          /* 1 */
    output(__builtin_popcount(0xff));       /* 8 */
    output(__builtin_popcount(0xffffffff)); /* 32 */
    output(__builtin_popcount(0x55555555)); /* 16 */
}
