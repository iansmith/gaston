/* fact.cm — recursive factorial
   input: n
   output: n! */

int fact(int n) {
    if (n == 0) return 1;
    return n * fact(n - 1);
}

void main(void) {
    output(fact(input()));
}
