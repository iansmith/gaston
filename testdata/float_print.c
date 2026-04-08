/* float_print: print_double runtime function */
int main(void) {
    double x;
    x = 3.0;
    print_double(x);        /* 3.000000 */
    x = 0.5;
    print_double(x);        /* 0.500000 */
    x = -1.25;
    print_double(x);        /* -1.250000 */
    x = 100.0;
    print_double(x);        /* 100.000000 */
    return 0;
}
