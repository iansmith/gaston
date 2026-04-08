void main(void) {
    /* bswap32(0x12345678) = 0x78563412 = 2018915346 */
    output(__builtin_bswap32(0x12345678));
    /* bswap16(0x1234) = 0x3412 = 13330 */
    output(__builtin_bswap16(0x1234));
    /* bswap32(1) = 0x01000000 = 16777216 */
    output(__builtin_bswap32(1));
    /* bswap64(0x0000000100000000UL) = 0x0000000001000000 = 16777216 */
    output((long)__builtin_bswap64(0x0000000100000000UL));
}
