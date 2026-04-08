void add_one(int *p, int n) {
    int i;
    i = 0;
    while (i < n) {
        p[i] = p[i] + 1;
        i = i + 1;
    }
}

void double_it(int *p, int n) {
    int i;
    i = 0;
    while (i < n) {
        p[i] = p[i] * 2;
        i = i + 1;
    }
}

int main(void) {
    int *p;
    int i;
    p = malloc(24);
    p[0] = 5;
    p[1] = 10;
    p[2] = 15;
    add_one(p, 3);
    double_it(p, 3);
    i = 0;
    while (i < 3) {
        output(p[i]);
        i = i + 1;
    }
    free(p);
    return 0;
}
