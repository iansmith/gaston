/* See weak_armem.c. */
extern int g_weak;

void main(void) {
    output(g_weak); /* 3 — archive member with only a weak def must be pulled */
}
