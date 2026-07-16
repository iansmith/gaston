/* Cross-TU tentative size mismatch, TU B: the larger declaration. */

long buf[4];
long guard;

void fill(void) {
    buf[0] = 5;
    buf[3] = 7;
    guard = 1;
}

long get3(void) {
    return buf[3];
}

/* guard must not live inside buf's 32-byte extent. */
int disjoint(void) {
    long b = (long)buf;
    long g = (long)&guard;
    if (g >= b + 32) return 1;
    if (g + 8 <= b) return 1;
    return 0;
}
