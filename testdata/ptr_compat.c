/* ptr_compat.cm — valid pointer assignments: void*↔any, same type, null constant */
void main(void) {
    int x;
    int *p;
    void *v;
    x = 99;
    p = &x;          /* int* = int*   ✓ */
    v = p;           /* void* = int*  ✓ */
    p = v;           /* int* = void*  ✓ */
    output(*p);      /* 99 */
    p = 0;           /* null pointer constant ✓ */
    p = &x;
    output(*p);      /* 99 */
}
