/* struct_basic.cm — local struct variable, assign fields, read back */
struct Point { int x; int y; };

void main(void) {
    struct Point s;
    s.x = 10;
    s.y = 20;
    output(s.x);
    output(s.y);
    output(s.x + s.y);
}
