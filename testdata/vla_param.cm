/* vla_param.cm — VLA size comes from a function parameter */
int dotprod(int n) {
    int a[n];
    int b[n];
    int i;
    int s;
    i = 0;
    while (i < n) {
        a[i] = i + 1;
        b[i] = i + 1;
        i = i + 1;
    }
    s = 0;
    i = 0;
    while (i < n) {
        s = s + a[i] * b[i];
        i = i + 1;
    }
    return s;
}

void main(void) {
    output(dotprod(3));
    output(dotprod(4));
}
