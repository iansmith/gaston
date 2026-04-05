struct Inner {
    int a;
    int b;
};
struct Outer {
    struct Inner lo;
    struct Inner hi;
};

struct Inner make_inner(int a, int b) {
    struct Inner i;
    i.a = a;
    i.b = b;
    return i;
}

struct Outer make_outer(int a, int b, int c, int d) {
    struct Outer o;
    o.lo.a = a; o.lo.b = b;
    o.hi.a = c; o.hi.b = d;
    return o;
}

int main(void) {
    struct Outer o;
    struct Inner x;
    struct Inner y;
    struct Outer o2;
    o = make_outer(1, 2, 3, 4);
    x = make_inner(9, 8);
    y = o.lo;
    output(y.a);
    output(y.b);
    o.hi = x;
    output(o.hi.a);
    output(o.hi.b);
    o2 = make_outer(10, 20, 30, 40);
    o2.lo = o.hi;
    output(o2.lo.a);
    output(o2.lo.b);
    output(o2.hi.a);
    output(o2.hi.b);
    return 0;
}
