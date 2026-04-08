struct Point {
    int x;
};

struct Point make_point(int v) {
    struct Point p;
    p.x = v;
    return p;
}

int main(void) {
    struct Point q;
    q = make_point(42);
    output(q.x);
    return 0;
}
