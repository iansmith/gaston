/* variadic_ptr.cm — variadic function reading pointer args */

void print_n_strings(long n, ...) {
    long* ap;
    long s;
    long i;
    ap = __va_start();
    i = 0;
    while (i < n) {
        s = *ap;
        print_string(s);
        ap = ap + 1;
        i = i + 1;
    }
}

void main(void) {
    print_n_strings(2, "hello\n", "world\n");
    print_n_strings(1, "done\n");
}
