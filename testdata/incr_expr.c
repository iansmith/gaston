/* incr_expr.cm — x++/x--/++x/--x used as expressions, verify old-vs-new semantics */

void main(void) {
    int x;
    int y;

    /* postfix: y gets OLD value, x gets incremented */
    x = 10;
    y = x++;
    output(y);   /* 10 — old value */
    output(x);   /* 11 — incremented */

    /* postfix decrement: y gets OLD value */
    y = x--;
    output(y);   /* 11 — old value */
    output(x);   /* 10 — decremented */

    /* prefix: x gets incremented first, y gets NEW value */
    y = ++x;
    output(y);   /* 11 — new value */
    output(x);   /* 11 */

    /* prefix decrement */
    y = --x;
    output(y);   /* 10 — new value */
    output(x);   /* 10 */

    /* use in arithmetic expression */
    x = 3;
    y = x++ + 1;
    output(y);   /* 4 — (old x=3) + 1 */
    output(x);   /* 4 — incremented */

    y = ++x + 1;
    output(y);   /* 6 — (new x=5) + 1 */
    output(x);   /* 5 */
}
