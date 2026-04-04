void fill(int *p, int n) {
    int i;
    i = 0;
    while (i < n) {
        p[i] = i * 2;
        i = i + 1;
    }
}

int main(void) {
    int *p;
    int i;
    p = malloc(48);
    fill(p, 6);
    i = 0;
    while (i < 6) {
        output(p[i]);
        i = i + 1;
    }
    free(p);
    return 0;
}
