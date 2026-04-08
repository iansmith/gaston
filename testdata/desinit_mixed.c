/* desinit_mixed.cm — mix of plain and designated entries; position tracking */
/* C99: plain entries advance from last designated position + 1 */
struct Four { int a; int b; int c; int d; };

void main(void) {
    /* .b=10 designates b (index 1), then plain 20 fills c (index 2) */
    struct Four q = { .b = 10, 20 };
    output(q.a);
    output(q.b);
    output(q.c);
    output(q.d);
}
