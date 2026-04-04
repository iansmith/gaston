/* struct_nested.cm — struct with 4 fields, tests larger offsets */
struct Rect { int x; int y; int w; int h; };

int area(struct Rect* r) {
    return r->w * r->h;
}

void main(void) {
    struct Rect r;
    r.x = 1;
    r.y = 2;
    r.w = 10;
    r.h = 20;
    output(r.x);
    output(r.y);
    output(r.w);
    output(r.h);
    output(area(&r));
}
