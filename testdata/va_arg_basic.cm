/* va_arg_basic.cm — exercise va_arg with int, double, and char* types.
   Verifies that va_arg correctly reads each argument and advances the
   va_list pointer by exactly one 8-byte slot per call. */
#include <stdarg.h>

int sum_ints(int n, ...) {
    va_list ap;
    int i;
    long s;
    va_start(ap, n);
    s = 0;
    i = 0;
    while (i < n) {
        s = s + va_arg(ap, long);
        i = i + 1;
    }
    va_end(ap);
    return s;
}

double sum_doubles(int n, ...) {
    va_list ap;
    int i;
    double s;
    va_start(ap, n);
    s = 0.0;
    i = 0;
    while (i < n) {
        s = s + va_arg(ap, double);
        i = i + 1;
    }
    va_end(ap);
    return s;
}

/* return the n-th string (0-based) from a variadic list of char* */
char* pick_string(int n, ...) {
    va_list ap;
    int i;
    char* s;
    va_start(ap, n);
    s = 0;
    i = 0;
    while (i <= n) {
        s = va_arg(ap, char*);
        i = i + 1;
    }
    va_end(ap);
    return s;
}

void main(void) {
    output(sum_ints(3, 10, 20, 30));            /* 60 */
    output(sum_ints(4, 1, 2, 3, 4));            /* 10 */
    print_double(sum_doubles(2, 1.5, 2.5));     /* 4.000000 */
    print_double(sum_doubles(3, 1.0, 2.0, 3.0));/* 6.000000 */
    print_string(pick_string(0, "alpha\n", "beta\n", "gamma\n")); /* alpha */
    print_string(pick_string(2, "alpha\n", "beta\n", "gamma\n")); /* gamma */
}
