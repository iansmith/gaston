struct Inner { int a; int b; };
struct Outer { struct Inner inner; };

void main(void) {
    struct Outer *o = &(struct Outer){ .inner = { .a = 7, .b = 8 } };
    output(o->inner.a);
    output(o->inner.b);
}
