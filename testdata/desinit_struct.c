/* desinit_struct.cm — .field designators on a 3-field struct; all fields; out-of-order */
struct Point { int x; int y; int z; };

void main(void) {
    struct Point p = { .z = 30, .x = 10, .y = 20 };
    output(p.x);
    output(p.y);
    output(p.z);
}
