struct Pair {
    int x;
    int y;
};

struct Pair swap(struct Pair p) {
    struct Pair r;
    r.x = p.y;
    r.y = p.x;
    return r;
}

int main(void) {
    struct Pair p;
    struct Pair q;
    p.x = 5;
    p.y = 9;
    q = swap(p);
    output(q.x);
    output(q.y);
    return 0;
}
