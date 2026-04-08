int mymin(int a, int b) {
    return ({ typeof(a) _a = a; typeof(b) _b = b; _a < _b ? _a : _b; });
}
void main(void) {
    output(mymin(3, 7));
    output(mymin(9, 7));
}
