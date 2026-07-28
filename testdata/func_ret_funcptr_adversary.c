/* Adversary gap coverage for GAST-22: function declarators returning a
 * function pointer. Exercises: a bare prototype (the literal musl signal.h
 * shape — declared with no body before the definition), a zero-parameter
 * outer signature, a direct chained call with no intermediate variable, and
 * an outer pointer-to-"function-returning-function-pointer" (one more level
 * of indirection than the base sigset shape, and the abstract/typed-variable
 * form of the same declarator). */

/* Bare prototype, no body — exactly musl signal.h's shape. */
void (*sigset(int, void (*)(int)))(int);

void handler_a(int x) { output(x); }
void handler_b(int x) { output(x * 2); }
void handler_c(int x) { output(x * 3); }

void (*sigset(int which, void (*h)(int)))(int) {
    return which ? handler_b : h;
}

/* Zero outer parameters. */
void (*no_args(void))(int) {
    return handler_c;
}

void main(void) {
    void (*r)(int) = sigset(0, handler_a);
    r(5);                      /* 5 */

    sigset(1, handler_a)(5);   /* 10 — direct chained call, no intermediate var */

    void (*(*fp)(int, void (*)(int)))(int) = sigset;
    void (*r2)(int) = fp(1, handler_a);
    r2(5);                     /* 10 */

    void (*z)(int) = no_args();
    z(5);                      /* 15 */
}
