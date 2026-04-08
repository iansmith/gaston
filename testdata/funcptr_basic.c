/* funcptr_basic.cm — basic function pointer: declare, assign, and call */
int add(int a, int b) { return a + b; }
int mul(int a, int b) { return a * b; }

int (*fp)(int, int);

void main(void) {
    fp = add;
    output(fp(3, 4));   /* 7 */
    fp = mul;
    output(fp(3, 4));   /* 12 */
    output(fp(5, 6));   /* 30 */
}
