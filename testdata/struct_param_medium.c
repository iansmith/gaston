struct Pair {
    int x;
    int y;
};

int sum_pair(struct Pair p) {
    return p.x + p.y;
}

int main(void) {
    struct Pair p;
    p.x = 12;
    p.y = 18;
    output(sum_pair(p));
    return 0;
}
