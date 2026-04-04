/* double_ptr_ops.cm — double* array: pointer arithmetic, deref stores/loads.
   Byzantine test for items 1+4: TypeDoublePtr with elemSize=8,
   IRFDerefStore/Load via pointer, *(p+k) dereference with k>0,
   pointer difference-by-assignment.

   arr filled: [1.0, 2.0, 3.0, 4.0, 5.0]
   Reads: arr[0], arr[2], arr[0]+arr[4], arr[1]+arr[2]+arr[3]

   Expected: 1.000000 3.000000 6.000000 9.000000 */

double arr[5];

void fill(double *p, int n) {
    int i;
    i = 0;
    while (i < n) {
        *p = i + 1;
        p = p + 1;
        i = i + 1;
    }
}

void main(void) {
    double *p;
    fill(&arr, 5);

    p = &arr;
    print_double(*p);              /* arr[0] = 1.000000 */

    p = &arr;
    p = p + 2;
    print_double(*p);              /* arr[2] = 3.000000 */

    p = &arr;
    print_double(*p + *(p + 4));   /* arr[0]+arr[4] = 1+5 = 6.000000 */

    p = &arr;
    p = p + 1;
    print_double(*p + *(p+1) + *(p+2));  /* arr[1]+arr[2]+arr[3] = 2+3+4 = 9.000000 */
}
