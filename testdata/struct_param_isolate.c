struct Pair {
    int x;
    int y;
};

void mutate(struct Pair p) {
    p.x = 999;
    p.y = 999;
}

int main(void) {
    struct Pair p;
    p.x = 3;
    p.y = 4;
    mutate(p);
    output(p.x + p.y);
    return 0;
}
