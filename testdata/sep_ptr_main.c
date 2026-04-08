extern void double_all(int *p, int n);

int main(void) {
    int *arr;
    int i;
    arr = malloc(40);
    i = 0;
    while (i < 5) {
        arr[i] = i + 1;
        i = i + 1;
    }
    double_all(arr, 5);
    i = 0;
    while (i < 5) {
        output(arr[i]);
        i = i + 1;
    }
    free(arr);
    return 0;
}
