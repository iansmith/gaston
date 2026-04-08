/* ptr_float.cm — float* pointer: address-of float, deref yields float, assign through.
   Tests item 1 (TypeFloatPtr as a proper pointer type distinct from TypeIntPtr):
   - *p has type float (not int), so print_double receives the correct FP value
   - Assigning through float* updates the underlying float variable */
float val;
float *p;

void main(void) {
    val = 2.5;
    p = &val;
    print_double(*p);   /* 2.500000 — deref float* yields float */
    *p = 7.25;
    print_double(val);  /* 7.250000 — assign through float* updates val */
    print_double(*p);   /* 7.250000 — re-read through pointer */
}
