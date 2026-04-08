/* ptr_triple_chain.cm — int*** via a three-level typedef chain.
   Exercises TypePtr(TypePtr(TypePtr(TypeInt))) all the way through:
   declaration, assignment, reading (***), and two-level write (**). */

typedef int*    Depth1;
typedef Depth1* Depth2;
typedef Depth2* Depth3;

int read3(Depth3 ppp) {
    return ***ppp;
}

void write1(Depth3 ppp, int v) {
    /* **ppp is a Depth1 (int*); ***ppp is the int it points at */
    ***ppp = v;
}

void main(void) {
    int    x;
    int    y;
    Depth1 p;
    Depth2 pp;
    Depth3 ppp;

    x   = 10;
    y   = 20;
    p   = &x;
    pp  = &p;
    ppp = &pp;

    output(read3(ppp));    /* 10 */

    /* write through all three levels of indirection */
    write1(ppp, 30);
    output(x);             /* 30 — x was modified through ***ppp */
    output(***ppp);        /* 30 */

    /* redirect p to y, watch the chain follow */
    *pp = &y;              /* p = &y */
    output(***ppp);        /* 20 */

    y = 77;
    output(read3(ppp));    /* 77 */
}
