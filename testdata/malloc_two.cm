int *a;
int *b;
int main(void) {
    int i;
    a = malloc(40);
    b = malloc(40);
    i = 0;
    while (i < 5) {
        a[i] = i + 1;
        b[i] = (i + 1) * 10;
        i = i + 1;
    }
    i = 0;
    while (i < 5) {
        output(a[i] + b[i]);
        i = i + 1;
    }
    free(a);
    free(b);
    return 0;
}
