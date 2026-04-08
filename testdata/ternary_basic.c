int sign(int x) {
    return (x > 0) ? 1 : ((x < 0) ? -1 : 0);
}

int abs_val(int x) {
    return (x < 0) ? -x : x;
}

int max2(int a, int b) {
    return (a > b) ? a : b;
}

void main(void) {
    output(sign(-5));
    output(sign(0));
    output(sign(3));
    output(abs_val(-7));
    output(abs_val(4));
    output(max2(3, 8));
    output(max2(10, 2));
}
