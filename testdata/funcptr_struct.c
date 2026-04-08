/* funcptr_struct.cm — function pointer called through a struct field */

int double_it(int x) {
    return x * 2;
}

int triple_it(int x) {
    return x * 3;
}

struct ops {
    int (*transform)(int);
    int base;
};

void main(void) {
    struct ops o;
    int result;

    o.transform = double_it;
    o.base = 21;
    result = o.transform(o.base);
    output(result);

    o.transform = triple_it;
    result = o.transform(o.base);
    output(result);
}
