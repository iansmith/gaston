/* ptr_ptr.cm — int** double pointer: create, assign through, dereference */
void set_ptr(int **pp, int *p) {
    *pp = p;
}

void main(void) {
    int x;
    int y;
    int *p;
    int **pp;
    x = 42;
    y = 99;
    p = &x;
    pp = &p;
    output(**pp);
    set_ptr(pp, &y);
    output(**pp);
    output(*p);
}
