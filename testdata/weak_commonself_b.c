/* See weak_commonself_a.c. */
int cval;
int getcv(void);

void main(void) {
    output(getcv()); /* 0 — COMMON wins; A's self-read sees the winner */
}
