/* Parenthesized function-type typedef: typedef ssize_t (name)(params);
 * musl's stdio.h cookie-I/O typedefs use this shape (redundant parens
 * around the name, no leading star). */

typedef int (myfunc_t)(int, int);

int adder(int a, int b) {
    return a + b;
}

void main(void) {
    myfunc_t *fn = adder;
    output(fn(19, 23)); /* 42 */
}
