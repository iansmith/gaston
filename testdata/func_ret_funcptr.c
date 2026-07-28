/* Function declarator returning a function pointer — the "sigset" shape from
 * musl's signal.h: T (*name(params))(params2), e.g.
 * void (*sigset(int, void (*)(int)))(int). Grammar previously had no
 * production for this recursive declarator form. */

void handler_a(int x) { output(x); }
void handler_b(int x) { output(x * 2); }

void (*choose(int which, void (*h)(int)))(int) {
    if (which)
        return handler_b;
    return h;
}

void main(void) {
    void (*a)(int) = choose(0, handler_a);
    a(5);                /* 5 */
    void (*b)(int) = choose(1, handler_a);
    b(5);                /* 10 */
}
