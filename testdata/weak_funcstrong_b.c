/* See weak_funcstrong_a.c. */
int pick(void) { return 2; }

void main(void) {
    output(pick()); /* 2 — strong function definition wins */
}
