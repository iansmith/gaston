/* desinit_nested.cm — struct with a struct field: { .inner = { .x = 1, .y = 2 } } */
struct Inner { int x; int y; };
struct Outer { int a; struct Inner inner; int b; };

void main(void) {
    struct Outer o = { .a = 10, .inner = { .x = 1, .y = 2 }, .b = 99 };
    output(o.a);
    output(o.inner.x);
    output(o.inner.y);
    output(o.b);
}
