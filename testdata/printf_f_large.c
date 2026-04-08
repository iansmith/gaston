/* printf_f_large.cm — verify %f always uses fixed notation regardless of magnitude */
#include <stdio.h>

int main(void) {
    printf("%f\n", 1234567890.0);
    printf("%f\n", 9876543210.5);
    printf("%f\n", -12345678901.0);
    printf("%.2f\n", 1000000000.125);
    return 0;
}
