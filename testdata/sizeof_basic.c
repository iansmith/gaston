/* sizeof_basic.cm — sizeof(type), sizeof(expr), sizeof(struct).
   LP64: int=4, char=1, pointer=8.  struct Pair { int a; int b; } = 8 bytes. */
struct Pair { int a; int b; };

void main(void) {
    int x;
    char c;
    int *p;
    struct Pair pr;
    output(sizeof(int));         /* 4 — LP64 */
    output(sizeof(char));        /* 1 */
    output(sizeof(x));           /* 4  — int var */
    output(sizeof(c));           /* 1  — char var */
    output(sizeof(p));           /* 8  — int* pointer */
    output(sizeof(struct Pair)); /* 8  — a@0(4)+b@4(4) */
    output(sizeof(pr));          /* 8  — struct Pair var */
}
