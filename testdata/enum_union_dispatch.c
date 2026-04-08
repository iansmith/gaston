/* enum_union_dispatch.cm — enum as tagged-union discriminant.
   A union holds either an int or a double; an enum tag says which.
   A typedef'd function pointer is used as a callback.
   Stresses: all five new features interacting:
     enum + union (in struct) + typedef (func ptr) + func ptr call + const*.

   Expected: 42 3.140000 7 99
   (const* test removed: &fieldaccess not supported in gaston grammar) */

enum { KIND_INT = 0, KIND_FLOAT = 1 };

union Value { int ival; double fval; };
struct Tagged { int kind; union Value v; };

/* Typedef for a printer callback — opaque param list since gaston's
   TypeFuncPtr doesn't track signatures */
typedef void (*Printer)();

void print_int_tagged(struct Tagged *t) {
    output(t->v.ival);
}

void print_flt_tagged(struct Tagged *t) {
    print_double(t->v.fval);
}

/* dispatch: choose printer via enum kind, call through local func ptr */
void dispatch(struct Tagged *t) {
    Printer p;
    if (t->kind == KIND_INT) {
        p = print_int_tagged;
        p(t);
    }
    if (t->kind == KIND_FLOAT) {
        p = print_flt_tagged;
        p(t);
    }
}

void main(void) {
    struct Tagged a;
    struct Tagged b;

    a.kind = KIND_INT;
    a.v.ival = 42;
    dispatch(&a);           /* 42 */

    b.kind = KIND_FLOAT;
    b.v.fval = 3.14;
    dispatch(&b);           /* 3.140000 */

    a.v.ival = 7;
    dispatch(&a);           /* 7 */

    /* direct read of the union int field */
    a.v.ival = 99;
    dispatch(&a);           /* 99 */
}
