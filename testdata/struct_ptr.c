/* struct_ptr.cm — pointer to struct, -> field access, pass to function */
struct Point { int x; int y; };

void set_point(struct Point* p, int x, int y) {
    p->x = x;
    p->y = y;
}

int sum_point(struct Point* p) {
    return p->x + p->y;
}

void main(void) {
    struct Point* p;
    p = malloc(16);
    set_point(p, 3, 7);
    output(p->x);
    output(p->y);
    output(sum_point(p));
    free(p);
}
