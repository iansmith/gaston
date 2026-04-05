struct Inner {
    int value;
};

struct Outer {
    struct Inner inner;
};

int get_value(struct Inner i) {
    return i.value;
}

int main(void) {
    struct Outer o;
    o.inner.value = 42;
    output(get_value(o.inner));
    return 0;
}
