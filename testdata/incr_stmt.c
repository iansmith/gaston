/* incr_stmt.cm — x++/x--/++x/--x used as statements (return value discarded) */

void main(void) {
    int x;
    x = 5;
    x++;
    output(x);   /* 6 */
    x--;
    output(x);   /* 5 */
    ++x;
    output(x);   /* 6 */
    --x;
    output(x);   /* 5 */
}
