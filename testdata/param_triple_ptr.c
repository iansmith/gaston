/* Triple-pointer parameter: char ***mem — three levels of pointer indirection
 * plus a name. The grammar hand-enumerates pointer depth per base-type
 * category and used to stop at two stars for plain type_specifier. */

int deref3(char ***mem) {
    return (int)***mem;
}

void main(void) {
    char c = 'A';
    char *p1 = &c;
    char **p2 = &p1;
    char ***p3 = &p2;
    output(deref3(p3)); /* 65 */
}
