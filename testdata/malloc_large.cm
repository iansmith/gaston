int main(void) {
    int *p;
    int i;
    int sum;
    p = malloc(800);
    i = 0;
    while (i < 100) {
        p[i] = i + 1;
        i = i + 1;
    }
    sum = 0;
    i = 0;
    while (i < 100) {
        sum = sum + p[i];
        i = i + 1;
    }
    output(sum);
    free(p);
    return 0;
}
