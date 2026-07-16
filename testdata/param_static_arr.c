/* C99 [static N] array parameters — including constant-expression sizes,
 * in both prototypes and definitions (musl: char buf[static 15+3*sizeof(int)]). */

void fill(char buf[static 4 + 3*sizeof(int)], int v);

void fill(char buf[static 4 + 3*sizeof(int)], int v) {
    buf[0] = v;
    buf[15] = v + 1;
}

void main(void) {
    char buf[16];
    fill(buf, 5);
    output(buf[0]);    /* 5 */
    output(buf[15]);   /* 6 */
}
