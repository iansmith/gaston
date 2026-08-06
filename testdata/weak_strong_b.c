/* Weak def coexists with a strong def of the same name, TU B (strong). The
 * strong definition must win, and linking must NOT fail as a duplicate
 * definition (weak defs are exempt from duplicate detection). */
int val = 2;

void main(void) {
    output(val); /* 2 — strong wins over weak */
}
