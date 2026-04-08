/* ptr_param.cm — function receives int* and mutates the caller's variable */
void increment(int *p) {
    *p = *p + 1;
}

void main(void) {
    int x;
    x = 10;
    increment(&x);
    increment(&x);
    output(x);
}
