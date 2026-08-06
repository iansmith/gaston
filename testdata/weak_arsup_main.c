/* See weak_arsup_a.c. */
extern int gsup;

void main(void) {
    output(gsup); /* 5 — the user object's weak def, not the archive's 9 */
}
