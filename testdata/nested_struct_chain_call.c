struct Pt {
    int x;
    int y;
};
struct Seg {
    struct Pt p;
    struct Pt q;
};

struct Pt make_pt(int x, int y) {
    struct Pt p;
    p.x = x;
    p.y = y;
    return p;
}

struct Pt midpoint(int ax, int ay, int bx, int by) {
    struct Pt m;
    m.x = (ax + bx) / 2;
    m.y = (ay + by) / 2;
    return m;
}

struct Seg make_seg(int ax, int ay, int bx, int by) {
    struct Seg s;
    s.p.x = ax;
    s.p.y = ay;
    s.q.x = bx;
    s.q.y = by;
    return s;
}

int main(void) {
    struct Seg s;
    struct Pt mid;
    struct Seg s2;
    s = make_seg(0, 0, 10, 4);
    mid = midpoint(s.p.x, s.p.y, s.q.x, s.q.y);
    output(mid.x);
    output(mid.y);
    s2 = make_seg(mid.x, mid.y, s.q.x, s.q.y);
    output(s2.p.x);
    output(s2.p.y);
    output(s2.q.x);
    output(s2.q.y);
    return 0;
}
