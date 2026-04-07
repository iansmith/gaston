int main(void) {
    void *p;
    p = sbrk(0);
    if ((long)p > 0) {
        output(1);
    } else {
        output(0);
    }
    p = sbrk(4096);
    if ((long)p > 0) {
        output(2);
    } else {
        output(0);
    }
    return 0;
}
