/* setjmp_basic.c — test setjmp/longjmp built-in helpers
 * jmp_buf layout: 22 × long long (AArch64 picolibc-compatible).
 * jmp_buf decays to long long* in function calls, so we declare
 * setjmp/longjmp with explicit pointer types. */

int  setjmp(long long *env);
void longjmp(long long *env, int val);

long long buf[22];

void jump_back(int v) {
    longjmp(buf, v);
}

int main(void) {
    int r;

    /* First setjmp call returns 0. */
    r = setjmp(buf);
    if (r == 0) {
        output(0);          /* prints 0 */
        jump_back(42);
        output(-1);         /* unreachable */
    } else {
        output(r);          /* prints 42 after longjmp */
    }

    /* longjmp(buf, 0) must return 1, not 0. */
    r = setjmp(buf);
    if (r == 0) {
        longjmp(buf, 0);
        output(-1);         /* unreachable */
    } else {
        output(r);          /* must print 1 */
    }

    return 0;
}
