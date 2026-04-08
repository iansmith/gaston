/* err_func_typedef_arg.cm — a function's parameter type is a struct-pointer
   typedef.  Calling it with a typedef that resolves to a *different* struct
   pointer must be caught at the call site, even though:
     (a) both typedef names end in "Ptr",
     (b) both structs have an identical single-int-field layout,
     (c) the error only surfaces after resolving both typedef chains. */

struct Alpha { int val; };
struct Beta  { int val; };

typedef struct Alpha* AlphaPtr;
typedef struct Beta*  BetaPtr;

void process(AlphaPtr a) {
    output(a->val);
}

void main(void) {
    struct Beta b;
    BetaPtr bp;

    b.val = 42;
    bp = &b;
    process(bp);   /* ERROR: arg 1 is BetaPtr(=Beta*), expected AlphaPtr(=Alpha*) */
}
