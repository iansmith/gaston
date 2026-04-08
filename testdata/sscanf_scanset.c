/* sscanf_scanset.cm — test P1-C %[...] scanset */
#include <stdio.h>

int main(void) {
    char buf[64];
    char first[32];
    char second[32];
    int n;

    /* basic range: stop at non-lowercase */
    n = sscanf("hello world", "%[a-z]", buf);
    printf("n=%d s=%s\n", n, buf);

    /* negated set: stop at space */
    n = sscanf("hello world", "%[^ ]", buf);
    printf("n=%d s=%s\n", n, buf);

    /* digit range */
    n = sscanf("12345abc", "%[0-9]", buf);
    printf("n=%d s=%s\n", n, buf);

    /* alphanumeric range */
    n = sscanf("abc123 end", "%[a-zA-Z0-9]", buf);
    printf("n=%d s=%s\n", n, buf);

    /* two-field split on space */
    n = sscanf("hello world", "%[^ ] %[^ ]", first, second);
    printf("n=%d a=%s b=%s\n", n, first, second);

    /* suppressed field */
    n = sscanf("skip rest", "%*[^ ] %[^ ]", buf);
    printf("n=%d s=%s\n", n, buf);

    return 0;
}
