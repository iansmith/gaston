/* incr_arr.cm — ++ and -- on array elements and through derefs */

void main(void) {
    int arr[4];
    int* p;
    int i;

    arr[0] = 10;
    arr[1] = 20;
    arr[2] = 30;
    arr[3] = 40;

    /* postfix on array element */
    i = arr[0]++;
    output(i);       /* 10 — old value */
    output(arr[0]);  /* 11 */

    /* prefix on array element */
    i = ++arr[1];
    output(i);       /* 21 — new value */
    output(arr[1]);  /* 21 */

    /* increment through pointer deref: ++*p */
    p = &arr;
    p++;
    p++;              /* p now points to arr[2] */
    i = ++*p;
    output(i);       /* 31 — new value */
    output(arr[2]);  /* 31 */

    /* decrement through pointer deref: --*p */
    i = --*p;
    output(i);       /* 30 — new value */
    output(arr[2]);  /* 30 */
}
