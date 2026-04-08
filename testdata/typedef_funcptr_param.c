/* typedef_funcptr_param.cm — typedef'd function pointer type used as variable
   type, function parameter type, and in cross-function dispatch.
   Stresses: TYPENAME in type_specifier, TYPENAME in param, indirect call
   through a parameter slot vs a global slot, passing named function directly.

   Expected: 7 12 15 5 25 */

typedef int (*BinOp)(int, int);

int add(int a, int b) { return a + b; }
int sub(int a, int b) { return a - b; }
int mul(int a, int b) { return a * b; }

/* apply receives a typedef'd func ptr parameter and calls through it */
int apply(BinOp op, int x, int y) {
    return op(x, y);
}

/* compose: apply op twice — op(op(x, y), y) */
int compose(BinOp op, int x, int y) {
    int mid;
    mid = op(x, y);
    return op(mid, y);
}

BinOp gop;

void main(void) {
    BinOp f;
    f = add;
    output(apply(f, 3, 4));        /* add(3,4)=7 */

    f = mul;
    output(apply(f, 3, 4));        /* mul(3,4)=12 */

    output(apply(add, 10, 5));     /* passing function name directly: 15 */
    output(apply(sub, 10, 5));     /* 5 */

    /* compose: mul(mul(5,5),5) = mul(25,5)=125? No: compose(mul,5,5)=mul(mul(5,5),5)=125 */
    /* Let's do add: compose(add,5,5)=add(add(5,5),5)=add(10,5)=15 */
    /* Use mul for squaring: compose(mul,5,5)=mul(mul(5,5),5)=125 — too big for this test */
    /* Use sub: compose(sub,10,3)=sub(sub(10,3),3)=sub(7,3)=4 — nah */
    /* Simple: gop global func ptr */
    gop = mul;
    output(gop(5, 5));             /* 25 */
}
