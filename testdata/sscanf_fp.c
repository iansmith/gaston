/* sscanf_fp.cm — test P1-A float precision and P1-B inf/nan scanning */
#include <stdio.h>

int main(void) {
    double x;
    int n;

    /* P1-A: integer-accumulation precision */
    n = sscanf("3.14159265", "%lf", &x);
    printf("n=%d v=%.8f\n", n, x);

    n = sscanf("0.001234", "%lf", &x);
    printf("n=%d v=%.6f\n", n, x);

    n = sscanf("-2.71828", "%lf", &x);
    printf("n=%d v=%.5f\n", n, x);

    n = sscanf("1.5e2", "%lf", &x);
    printf("n=%d v=%.0f\n", n, x);

    n = sscanf(".75", "%lf", &x);
    printf("n=%d v=%.2f\n", n, x);

    /* P1-B: inf / nan */
    n = sscanf("inf", "%lf", &x);
    printf("n=%d v=%f\n", n, x);

    n = sscanf("-INF", "%lf", &x);
    printf("n=%d v=%f\n", n, x);

    n = sscanf("nan", "%lf", &x);
    printf("n=%d v=%f\n", n, x);

    n = sscanf("NAN", "%lf", &x);
    printf("n=%d v=%f\n", n, x);

    return 0;
}
