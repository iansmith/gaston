/* void_ptr_chain.cm — void* as a generic store threaded through three functions.
   Byzantine test for item 7: void* ↔ int*, void* ↔ double*, multiple
   round-trips through a global void* slot.  Mixes items 1 (TypeDoublePtr) and 7.

   Expected: 42 99 7.000000 */

void *slot;

void save(void *p) {
    slot = p;
}

int read_int(void) {
    int *p;
    p = slot;
    return *p;
}

double read_double(void) {
    double *p;
    p = slot;
    return *p;
}

void main(void) {
    int   x;
    double d;

    x = 42;
    save(&x);
    output(read_int());     /* 42 */

    x = 99;
    output(read_int());     /* 99 — slot still points at x */

    d = 7.0;
    save(&d);
    print_double(read_double());  /* 7.000000 */
}
