/* Anonymous bit-fields with constant-expression widths — the alltypes.h
 * padding idiom: `int :8*(sizeof(T)-sizeof(long))*(A==B);` (width 0 here).
 * Also anonymous nonzero-width (consumes bits, no member) and zero-width
 * (closes the storage unit). */

typedef long time2_t;

struct ts {
    time2_t sec;
    int :8*(sizeof(time2_t)-sizeof(long))*(1234==4321);   /* width 0 */
    long nsec;
    int :8*(sizeof(time2_t)-sizeof(long))*(1234!=4321);   /* width 0 */
};

struct mix {
    unsigned a:3;
    unsigned :5;   /* anonymous: consumes 5 bits, no member */
    unsigned b:3;
    unsigned :0;   /* zero-width: close the storage unit */
    unsigned c:3;
};

void main(void) {
    struct ts t;
    struct mix m;

    t.sec = 5;
    t.nsec = 7;
    output((int)sizeof(struct ts));   /* 16 */
    output((int)t.sec);               /* 5 */
    output((int)t.nsec);              /* 7 */

    m.a = 5;
    m.b = 6;
    m.c = 7;
    output(m.a);                      /* 5 */
    output(m.b);                      /* 6 — after 5 anonymous bits */
    output(m.c);                      /* 7 — in a fresh storage unit */
}
