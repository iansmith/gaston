/* sizeof_array.cm — sizeof for local and global arrays (item 6).
   Local array: sizeof(arr) = N × 8.
   Array param: sizeof(param) = 8 (decays to pointer).
   Global array: sizeof(garr) = N × 8. */
int garr[3];

int param_size(int a[]) {
    return sizeof(a);     /* 8 — array param decays to pointer */
}

int local_size(void) {
    int arr[5];
    return sizeof(arr);   /* 40 = 5 × 8 */
}

void main(void) {
    output(sizeof(garr));       /* 24 = 3 × 8 */
    output(local_size());       /* 40 */
    output(param_size(garr));   /* 8  — param decay */
}
