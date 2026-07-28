/* Bare (nameless) struct/union value parameters — the sigqueue(pid_t, int,
 * union sigval) prototype shape. Named forms (struct T x) and pointer forms
 * (struct T *) already parse; the bare value form did not, for either
 * STRUCT or UNION. */

union sigval {
    int sival_int;
};

struct point {
    int x;
};

int take_union(int tag, union sigval);
int take_struct(int tag, struct point);

void main(void) {
    union sigval v;
    v.sival_int = 7;
    output(take_union(1, v)); /* 8 */

    struct point p;
    p.x = 34;
    output(take_struct(1, p)); /* 35 */
}

int take_union(int tag, union sigval v) {
    return tag + v.sival_int;
}

int take_struct(int tag, struct point p) {
    return tag + p.x;
}
