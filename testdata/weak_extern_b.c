/* Cross-TU weak definition, TU B. See weak_extern_a.c. */
extern int flag;

void main(void) {
    output(flag); /* 7 — must see A's weak definition, not VA 0 */
}
