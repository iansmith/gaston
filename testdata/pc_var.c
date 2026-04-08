/* pc_var.cm — print_char driven by a variable and arithmetic in a loop */
void main(void) {
    int i;
    int base;
    base = 65;
    for (i = 0; i < 5; i++) {
        print_char(base + i);
    }
    print_char(10);
}
