struct Pair {
    int a;
    int b;
};
struct Quad {
    struct Pair lo;
    struct Pair hi;
};

struct Pair swap_pair(int a, int b) {
    struct Pair p;
    p.a = b;
    p.b = a;
    return p;
}

int main(void) {
    struct Pair x;
    struct Pair y;
    struct Quad q;
    struct Quad q2;
    x.a = 11;
    x.b = 22;
    y = x;
    output(y.a);
    output(y.b);
    y = swap_pair(y.a, y.b);
    output(y.a);
    output(y.b);
    q.lo.a = 1; q.lo.b = 2;
    q.hi.a = 3; q.hi.b = 4;
    q2 = q;
    output(q2.lo.a);
    output(q2.lo.b);
    output(q2.hi.a);
    output(q2.hi.b);
    return 0;
}
