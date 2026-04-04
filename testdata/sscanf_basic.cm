/* sscanf_basic.cm — test sscanf with %d, %s, %c */
#include <stdio.h>

int main(void) {
    long n;
    char buf[32];
    long c;
    int r;

    r = sscanf("42", "%d", &n);
    printf("n=%d r=%d\n", n, r);

    r = sscanf("hello world", "%s", buf);
    printf("s=%s r=%d\n", buf, r);

    r = sscanf("  -7  99", "%d %d", &n, &c);
    printf("a=%d b=%d r=%d\n", n, c, r);

    r = sscanf("X", "%c", &c);
    printf("c=%c r=%d\n", c, r);

    return 0;
}
