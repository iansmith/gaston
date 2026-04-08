/* ptr_basic.cm — &x gives address, *p reads, *p = val writes back through original */
void main(void) {
    int x;
    int *p;
    x = 42;
    p = &x;
    output(*p);
    *p = 99;
    output(x);
}
