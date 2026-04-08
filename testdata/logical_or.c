int boom(void) {
    output(999);
    return 0;
}
int main(void) {
    int p;
    int q;
    /* basic || */
    output(0 || 1);
    output(0 || 0);
    output(1 || 0);
    /* short-circuit: 1 || boom() must not call boom() */
    output(1 || boom());
    /* in condition: p==0 but q!=0 → true */
    p = 0;
    q = 5;
    if (p != 0 || q != 0) {
        output(1);
    }
    return 0;
}
