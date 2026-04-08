/* char_arith.cm — char literal arithmetic: 'a' + i to print alphabet */
void main(void) {
    int i;
    for (i = 0; i < 5; i++) {
        print_char('a' + i);
    }
    print_char('\n');
}
