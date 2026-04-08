void main(void) {
    __uint128_t a = 1;
    __uint128_t b = 1;
    __uint128_t c;
    c = a + b;
    output((int)(a == b));
    output((int)(a < b));
    output((int)(c > a));
}
