static int helper(int x) { return x + 1; }
inline int square(int x) { return x * x; }
int main(void) {
    output(helper(5));
    output(square(4));
    return 0;
}
