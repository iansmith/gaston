/* vtable.cm — function pointer stored as a struct field; "vtable" dispatch pattern.
   Byzantine test for item 9: TypeFuncPtr in a struct field (IRFieldLoad/Store for
   TypeFuncPtr, which is 8-byte pointer-sized), loaded and called via local variable.
   Also stresses items 2 (struct layout with pointer field) and 3 (-> field access).

   struct Op { BinFn fn; int identity; }
   fn@0(8), identity@8(8), sizeof=16.

   Expected: 3 8 15 0 */

typedef int (*BinFn)(int, int);

struct Op {
    BinFn fn;
    int   identity;
};

int do_add(int a, int b) { return a + b; }
int do_sub(int a, int b) { return a - b; }
int do_mul(int a, int b) { return a * b; }

int run(struct Op *op, int x, int y) {
    BinFn f;
    f = op->fn;
    return f(x, y);
}

void main(void) {
    struct Op add_op;
    struct Op sub_op;
    struct Op mul_op;
    BinFn     f;

    add_op.fn = do_add;   add_op.identity = 0;
    sub_op.fn = do_sub;   sub_op.identity = 0;
    mul_op.fn = do_mul;   mul_op.identity = 1;

    output(run(&add_op, 1, 2));    /* 1+2=3 */
    output(run(&sub_op, 10, 2));   /* 10-2=8 */
    output(run(&mul_op, 3, 5));    /* 3*5=15 */

    /* Load fn from struct field into a local var, then call it */
    f = mul_op.fn;
    output(f(0, 7));               /* mul(0,7)=0 */
}
