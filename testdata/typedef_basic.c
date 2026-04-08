/* typedef_basic.cm — typedef for a scalar type and a pointer type */
typedef int myint;
typedef char *cstring;

void main(void) {
    myint x;
    myint y;
    x = 42;
    y = x + 8;
    output(x);  /* 42 */
    output(y);  /* 50 */
}
