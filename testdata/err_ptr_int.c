/* err_ptr_int.cm — assigning a non-zero integer to a pointer should be a semcheck error */
void main(void) {
    int *p;
    p = 42;   /* ERROR: non-zero integer assigned to pointer */
}
