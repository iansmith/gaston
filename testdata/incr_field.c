/* incr_field.cm — ++ and -- on struct fields */

struct counter {
    int val;
    int extra;
};

void main(void) {
    struct counter c;
    struct counter* p;
    int i;

    c.val = 5;
    c.extra = 100;

    /* postfix on struct field via dot */
    i = c.val++;
    output(i);     /* 5 — old value */
    output(c.val); /* 6 */

    /* prefix on struct field via dot */
    i = ++c.val;
    output(i);     /* 7 — new value */
    output(c.val); /* 7 */

    /* via arrow */
    p = &c;
    i = p->val--;
    output(i);     /* 7 — old value */
    output(p->val);/* 6 */

    i = --p->val;
    output(i);     /* 5 — new value */
    output(p->val);/* 5 */
}
