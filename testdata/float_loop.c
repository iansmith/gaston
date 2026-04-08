/* float_loop: float accumulation and multiplication in loops */
int main(void) {
    double sum;
    double prod;
    int i;
    int n;
    sum = 0.0;
    i = 0;
    while (i < 8) {
        sum = sum + 0.5;
        i = i + 1;
    }
    n = sum;
    output(n);   /* 4 */
    prod = 1.0;
    i = 0;
    while (i < 4) {
        prod = prod * 2.0;
        i = i + 1;
    }
    n = prod;
    output(n);   /* 16 */
    return 0;
}
