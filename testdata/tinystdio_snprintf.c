/* tinystdio_snprintf.cm — test tinystdio snprintf via Docker */

extern int snprintf(char *buf, long n, const char *fmt, ...);

void main(void) {
    char buf[200];

    print_char('A');
    print_char('\n');
    snprintf(buf, 200, "hello");
    print_char('B');
    print_char('\n');

    char *p;
    p = buf;
    while (*p) {
        print_char(*p);
        p = p + 1;
    }
    print_char('\n');
}
