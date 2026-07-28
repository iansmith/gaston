/* const-qualified multi-pointer struct field: two or more pointer members
 * declared in one CONST-qualified statement. The non-const version
 * (type_specifier '*' ID ',' ptr_id_list ';') already parses; the
 * CONST-prefixed block only had single-declarator forms. */

struct pair {
    const int *a, *b;
};

void main(void) {
    int x = 10;
    int y = 32;
    struct pair p;
    p.a = &x;
    p.b = &y;
    output(*p.a + *p.b); /* 42 */
}
