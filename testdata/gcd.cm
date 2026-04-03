/* gcd.cm — Euclid's GCD (from Louden's book)
   input: two integers u v
   output: gcd(u, v) */

int gcd(int u, int v) {
    if (v == 0) return u;
    return gcd(v, u - (u / v) * v);
}

void main(void) {
    int x;
    int y;
    x = input();
    y = input();
    output(gcd(x, y));
}
