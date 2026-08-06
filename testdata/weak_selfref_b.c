/* See weak_selfref_a.c. */
int selfv = 2;
int getself(void);

void main(void) {
    output(getself()); /* 2 — A's self-reference must resolve to B's strong def */
}
