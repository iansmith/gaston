int main(void) {
    int x;
    int y;
    int z;
    int *ptrs[3];
    x = 10;
    y = 20;
    z = 30;
    ptrs[0] = &x;
    ptrs[1] = &y;
    ptrs[2] = &z;
    output(*ptrs[0]);
    output(*ptrs[1]);
    output(*ptrs[2]);
    *ptrs[1] = 99;
    output(y);
    return 0;
}
