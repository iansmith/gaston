/* printf_simple.cm — debug float through va_list */

extern int snprintf(char *buf, long n, const char *fmt, ...);

void main(void) {
    char buf[100];

    /* Test integer */
    snprintf(buf, 100, "%d", 42);
    print_string(buf);
    print_char('\n');

    /* Test float */
    snprintf(buf, 100, "%f", 3.14);
    print_string(buf);
    print_char('\n');
}
