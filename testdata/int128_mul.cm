void main(void) {
    /* 2^63 * 2 = 2^64; high 64 bits should be 1 */
    unsigned long a = 0x8000000000000000UL;   /* 2^63 */
    unsigned long b = 2;
    __uint128_t r;
    unsigned long hi;
    r = (__uint128_t)a * b;
    hi = (unsigned long)(r >> 64);
    output((int)hi);
}
