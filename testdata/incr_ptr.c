/* incr_ptr.cm — ++ and -- on pointers (scaled by element size) */

void main(void) {
    int arr[5];
    int* p;
    int i;

    i = 0;
    while (i < 5) {
        arr[i] = i * 10;
        i++;
    }

    /* postfix pointer increment */
    p = &arr;
    output(*p);  /* 0 */
    p++;
    output(*p);  /* 10 */

    /* prefix pointer increment */
    ++p;
    output(*p);  /* 20 */

    /* postfix pointer decrement */
    p--;
    output(*p);  /* 10 */

    /* prefix pointer decrement */
    --p;
    output(*p);  /* 0 */

    /* pointer increment as expression: old pointer value used in deref */
    p = &arr;
    output(*p++);  /* 0  — deref old pointer, then advance */
    output(*p);    /* 10 — p now points to arr[1] */

    /* prefix as expression: advance first, then deref */
    output(*++p);  /* 20 — p advances to arr[2], then deref */
    output(*p);    /* 20 */
}
