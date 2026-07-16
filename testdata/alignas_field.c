/* _Alignas(N) on a struct MEMBER must pad the member to offset N*k, widen
 * the struct's total size to a multiple of its (widened) alignment, and
 * preserve member alignment across array elements (stride). */

struct P {
    char a;
    _Alignas(8) int b;
};

struct Q {
    char c;
    _Alignas(16) char d;
};

void main(void) {
    struct P p;
    struct Q q;
    struct P arr[2];

    output((int)sizeof(struct P));       /* 16 — b at 8, end 12, rounded to 8 → 16 */
    output((int)((long)&p.b % 8));       /* 0 */
    output((int)sizeof(struct Q));       /* 32 — d at 16, end 17, rounded to 16 */
    output((int)((long)&q.d % 16));      /* 0 */
    output((int)((long)&arr[1].b % 8));  /* 0 — stride keeps element alignment */
}
