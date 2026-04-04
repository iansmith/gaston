/* combo_all.cm — integration test: const, multi-decl, char, string, pointers */

const int N = 3;
const int NEWLINE = 10;

/* increment every element of the array pointed to by p */
void inc_all(int *p, int n) {
    int i;
    for (i = 0; i < n; i++) {
        *(p + i) = *(p + i) + 1;
    }
}

void main(void) {
    int arr[3];
    int *p;
    int i, total;
    char c;

    arr[0] = 10;
    arr[1] = 20;
    arr[2] = 30;

    p = &arr;
    inc_all(p, N);

    /* sum via normal array indexing after pointer-based increment */
    total = 0;
    for (i = 0; i < N; i++) {
        total = total + arr[i];
    }
    output(total);          /* 11+21+31 = 63 */

    /* char arithmetic using a const as upper bound */
    c = 'A';
    while (c < 'A' + N) {  /* N=3: prints A, B, C */
        print_char(c);
        c = c + 1;
    }
    print_char(NEWLINE);

    print_string("ok\n");
}
