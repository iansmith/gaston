/* printf_float.cm — test printf with %f, %.2f, %e, %g format specifiers */
#include <stdio.h>

int main(void) {
    printf("%f\n", 3.14);
    printf("%.2f\n", 2.71828);
    printf("%e\n", 12345.6789);
    printf("%g\n", 0.000123);
    printf("%g\n", 123456.0);
    return 0;
}
