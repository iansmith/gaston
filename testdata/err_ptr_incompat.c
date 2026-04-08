/* err_ptr_incompat.cm — assigning int* to char* should be a semcheck error */
void main(void) {
    int x;
    int *p;
    char *s;
    x = 1;
    p = &x;
    s = p;    /* ERROR: int* assigned to char* */
}
