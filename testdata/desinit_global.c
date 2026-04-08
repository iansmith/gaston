/* desinit_global.cm — global struct with constant designator initializers */
struct Vec { int x; int y; int z; };

struct Vec v = { .x = 1, .y = 2, .z = 3 };

void main(void) {
    output(v.x);
    output(v.y);
    output(v.z);
}
