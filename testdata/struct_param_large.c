struct Triple {
    int a;
    int b;
    int c;
};

int sum_triple(struct Triple t) {
    return t.a + t.b + t.c;
}

int main(void) {
    struct Triple t;
    t.a = 1;
    t.b = 2;
    t.c = 3;
    output(sum_triple(t));
    return 0;
}
