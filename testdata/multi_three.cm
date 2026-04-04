/* multi_three.cm — multi-decls in two functions; verify no slot interference */
int g1, g2;

void foo(void) {
    int x, y;
    x = 3;
    y = 4;
    g1 = x * y;
}

void main(void) {
    int a, b;
    a = 1;
    b = 2;
    g2 = a + b;
    foo();
    output(g1);
    output(g2);
    output(g1 + g2);
}
