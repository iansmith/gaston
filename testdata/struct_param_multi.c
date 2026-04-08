struct Point {
    int x;
    int y;
};

int dist2(struct Point a, struct Point b) {
    int dx;
    int dy;
    dx = b.x - a.x;
    dy = b.y - a.y;
    return dx*dx + dy*dy;
}

int main(void) {
    struct Point a;
    struct Point b;
    a.x = 0;
    a.y = 0;
    b.x = 3;
    b.y = 4;
    output(dist2(a, b));
    return 0;
}
