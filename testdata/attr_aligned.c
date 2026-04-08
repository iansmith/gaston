/* __attribute__((aligned)) on struct is parsed and ignored */
struct __attribute__((aligned(16))) aligned_s {
    int x;
    int y;
};

void main(void) {
    struct aligned_s s;
    s.x = 3;
    s.y = 7;
    output(s.x + s.y);           /* 10 */
    output(sizeof(struct aligned_s));  /* 8 (two ints) */
}
