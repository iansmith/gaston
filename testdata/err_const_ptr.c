/* err_const_ptr.cm — assignment through const pointer must be rejected */
void main(void) {
    int x;
    const int *p;
    x = 42;
    p = &x;
    *p = 99;  /* error: assignment to const-qualified pointer target */
}
