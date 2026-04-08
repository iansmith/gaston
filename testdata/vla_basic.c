/* vla_basic.cm — variable-length array: allocate int[n], fill, sum */
void fill(int arr[], int n) {
    int i;
    i = 0;
    while (i < n) {
        arr[i] = i * 2;
        i = i + 1;
    }
}

int vsum(int arr[], int n) {
    int i;
    int s;
    i = 0;
    s = 0;
    while (i < n) {
        s = s + arr[i];
        i = i + 1;
    }
    return s;
}

void test5(void) {
    int n = 5;
    int buf[n];
    fill(buf, n);
    output(vsum(buf, n));
}

void test4(void) {
    int n = 4;
    int buf[n];
    fill(buf, n);
    output(vsum(buf, n));
}

void main(void) {
    test5();
    test4();
}
