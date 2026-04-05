int boom(void) {
    output(999);
    return 0;
}
int main(void) {
    int x;
    /* bitwise & vs logical &&: (128 & 1) == 0 → 1; 128 && 1 → 1 */
    output((128 & 1) == 0);
    output(128 && 1);
    /* short-circuit: 0 && boom() must not call boom() */
    output(0 && boom());
    /* in condition */
    x = 5;
    if (x > 0 && x < 10) {
        output(1);
    }
    if (x > 0 && x > 10) {
        output(999);
    }
    return 0;
}
