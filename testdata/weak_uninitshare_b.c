/* See weak_uninitshare_a.c. */
extern int zshare;
int getz(void);

void main(void) {
    zshare = 6;
    output(getz()); /* 6 — B's write visible through A's read: one slot */
}
