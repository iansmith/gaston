/* See weak_func_a.c. */
int get(void);

void main(void) {
    output(get()); /* 7 — must call A's weak function definition */
}
