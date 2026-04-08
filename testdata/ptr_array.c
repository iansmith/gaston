/* ptr_array.cm — pointer arithmetic to address array elements (auto-scaled) */
void main(void) {
    int arr[3];
    int *p;
    arr[0] = 10;
    arr[1] = 20;
    arr[2] = 30;
    p = &arr;
    output(*p);
    output(*(p + 1));
    output(*(p + 2));
}
