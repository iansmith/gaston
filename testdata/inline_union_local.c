/* Test: inline anonymous union as local variable with initializer */
typedef unsigned int uint32_t;

static uint32_t asuint(float f) {
    union {
        float    f;
        uint32_t i;
    } u = { f };
    return u.i;
}

static float asfloat(uint32_t i) {
    union {
        float    f;
        uint32_t i;
    } u = { i };
    return u.f;
}

void main(void) {
    uint32_t bits = asuint(1.5);
    float    back = asfloat(bits);
    /* 1.5 as IEEE 754 bits = 0x3FC00000 */
    output(bits == 0x3FC00000 ? 1 : 0);
}
