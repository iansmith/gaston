/* short and unsigned short — stored as 64-bit in frame, arithmetic works */
int main(void) {
    short x;
    unsigned short y;
    x = 1000;
    y = 2000;
    output(x + y);
    output(x * 3);
    return 0;
}
