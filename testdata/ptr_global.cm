/* ptr_global.cm — pointer to a global variable, modified through the pointer */
int g;

void double_it(int *p) {
    *p = *p * 2;
}

void main(void) {
    g = 21;
    double_it(&g);
    output(g);
}
