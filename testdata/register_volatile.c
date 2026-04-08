int main(void) {
    register int x;
    volatile int y;
    x = 42;
    y = x + 1;
    output(x);
    output(y);
    return 0;
}
