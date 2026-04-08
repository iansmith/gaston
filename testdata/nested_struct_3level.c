struct Leaf {
    int v;
};
struct Mid {
    struct Leaf lo;
    struct Leaf hi;
};
struct Top {
    struct Mid m;
    int extra;
};

struct Top make_top(int lo, int hi, int ex) {
    struct Top t;
    t.m.lo.v = lo;
    t.m.hi.v = hi;
    t.extra = ex;
    return t;
}

int main(void) {
    struct Top t;
    t = make_top(10, 20, 99);
    output(t.m.lo.v);
    output(t.m.hi.v);
    output(t.extra);
    return 0;
}
