/* err_ptr_fp.cm — assigning double* to int* must be rejected by semcheck.
   Tests item 7: pointer assignment type checking for floating-point pointer types.
   TypeDoublePtr != TypeIntPtr and neither is void*, so this must be an error. */
void main(void) {
    double x;
    double *dp;
    int *ip;
    x = 1.0;
    dp = &x;
    ip = dp;   /* ERROR: double* assigned to int* — incompatible pointer types */
}
