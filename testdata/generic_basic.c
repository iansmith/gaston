#define type_id(x) _Generic((x), \
    int: 1, \
    long: 2, \
    double: 3, \
    default: 0)

void main(void) {
    int i = 0;
    long l = 0;
    double d = 0;
    output(type_id(i));       /* 1 */
    output(type_id(l));       /* 2 */
    output(type_id(d));       /* 3 */
    output(type_id(i + l));   /* 2 — int+long promotes to long */
}
