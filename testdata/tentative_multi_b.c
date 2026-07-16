/* Cross-TU tentative definitions, TU B. See tentative_multi_a.c. */

long both;          /* tentative here AND in tentative_multi_a.c */
long shared = 42;   /* real definition — must win over A's tentative */

long b_read_both(void) {
    return both;
}
