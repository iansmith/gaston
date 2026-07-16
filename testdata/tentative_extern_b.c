/* TU B: extern-only reference; the sole definition is A's tentative one. */

extern long tvar;

long get_tvar(void) {
    return tvar;
}
