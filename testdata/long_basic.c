/* long / long long — should behave identically to int on ARM64 */
int main(void) {
    long x;
    long long y;
    x = 1000000;
    y = 2000000;
    output(x + y);
    return 0;
}
