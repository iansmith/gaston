struct Vec2 {
    int x;
    int y;
};
struct Rect {
    struct Vec2 tl;
    struct Vec2 br;
};

struct Rect make_rect(int x0, int y0, int x1, int y1) {
    struct Rect r;
    r.tl.x = x0;
    r.tl.y = y0;
    r.br.x = x1;
    r.br.y = y1;
    return r;
}

int area(int x0, int y0, int x1, int y1) {
    return (x1 - x0) * (y1 - y0);
}

int main(void) {
    struct Rect r;
    r = make_rect(2, 3, 7, 8);
    output(r.tl.x);
    output(r.tl.y);
    output(r.br.x);
    output(r.br.y);
    output(area(r.tl.x, r.tl.y, r.br.x, r.br.y));
    return 0;
}
