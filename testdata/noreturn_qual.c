/* noreturn_qual.cm — C11/GCC qualifier keywords are silently accepted.
   Exercises: _Noreturn on a function declaration, __inline__ function,
   __restrict__ pointer parameter, __signed__ integer declaration.
   All qualifiers are dropped by the lexer's skipWords mechanism; the
   generated code must behave identically to the qualifier-free version.
   Expected: 42 21 50 -99 */

_Noreturn void emit(int n) {
    /* gaston ignores _Noreturn — function compiles and returns normally */
    output(n);
}

__inline__ int triple(int x) {
    /* gaston ignores __inline__ — regular function call emitted */
    return x + x + x;
}

int sum_restrict(int * __restrict__ p, int n) {
    /* __restrict__ dropped by skipWords; pointer semantics unchanged */
    int s;
    int i;
    s = 0;
    for (i = 0; i < n; i++) {
        s = s + p[i];
    }
    return s;
}

int main(void) {
    int arr[4];
    __signed__ int x;          /* __signed__ dropped; treated as plain int */

    arr[0] = 5;
    arr[1] = 10;
    arr[2] = 15;
    arr[3] = 20;
    x = -99;

    emit(42);                       /* 42  — _Noreturn function works fine */
    output(triple(7));              /* 21  — __inline__ function works fine */
    output(sum_restrict(arr, 4));   /* 50  — __restrict__ param works fine */
    output(x);                      /* -99 — __signed__ int works fine */
    return 0;
}
