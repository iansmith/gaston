/* ptr_arith.cm — pointer arithmetic auto-scaling (int* advances by 8 bytes per element) */
void main(void) {
    int arr[4];
    int *p;
    arr[0] = 10;
    arr[1] = 20;
    arr[2] = 30;
    arr[3] = 40;
    p = &arr;
    output(*(p + 0));
    output(*(p + 1));
    output(*(p + 2));
    output(*(p + 3));
    p = p + 2;
    output(*p);
    output(*(p - 1));
}
