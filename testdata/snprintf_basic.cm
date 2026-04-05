/* snprintf_basic.cm — test sprintf and snprintf */
#include <stdio.h>

int main(void) {
    char buf[64];
    int n;

    /* sprintf basic */
    sprintf(buf, "x=%d", 42);
    printf("%s\n", buf);

    /* sprintf with multiple specifiers */
    sprintf(buf, "%s=%f", "pi", 3.14159);
    printf("%s\n", buf);

    /* snprintf within capacity */
    n = snprintf(buf, 64, "hello %s", "world");
    printf("%s len=%d\n", buf, n);

    /* snprintf truncation: cap=6 leaves room for 5 chars + null */
    n = snprintf(buf, 6, "hello world");
    printf("%s len=%d\n", buf, n);

    /* snprintf n=1: only null byte written */
    buf[0] = 'X';
    n = snprintf(buf, 1, "abc");
    printf("empty='%s' len=%d\n", buf, n);

    return 0;
}
