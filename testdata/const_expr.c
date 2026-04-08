/* const_expr.cm — const in comparisons and if-conditions */
const int LIMIT = 10;

void main(void) {
    int x;
    x = 5;
    if (x < LIMIT) {
        output(1);
    } else {
        output(0);
    }
    x = 15;
    if (x < LIMIT) {
        output(1);
    } else {
        output(0);
    }
    output(LIMIT * 2);
}
