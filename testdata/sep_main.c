extern int add(int x, int y);
extern int mul(int x, int y);

int main(void) {
    int r;
    r = add(3, 4);
    output(r);
    r = mul(3, 4);
    output(r);
    return 0;
}
