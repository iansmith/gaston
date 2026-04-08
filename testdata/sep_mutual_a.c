extern int is_even(int n);

int is_odd(int n) {
    if (n == 0) return 0;
    return is_even(n - 1);
}

int main(void) {
    output(is_odd(7));
    output(is_odd(4));
    return 0;
}
