/* ptr_inc.cm — pointer ++/--/+=/−= with auto-scaling */
void main(void) {
    int arr[5];
    int *p;
    int i;
    i = 0;
    while (i < 5) {
        arr[i] = (i + 1) * 10;
        i = i + 1;
    }
    p = &arr;
    output(*p);
    p++;
    output(*p);
    p += 2;
    output(*p);
    p--;
    output(*p);
    p -= 2;
    output(*p);
}
