/* void_ptr_launder.cm — void* used as a universal adapter at each level.
   Each individual assignment is valid (void* accepts any pointer and vice
   versa), so the whole chain must compile cleanly and run correctly.
   This is the non-error counterpart to the byzantine error tests: the type
   checker must accept void* at every intermediate step even when the
   surrounding types are concrete and different-depth.
   Note: "typedef int** IntPtrPtr" requires two steps since grammar only
   supports one '*' per typedef; use typedef IntPtr* IntPtrPtr. */

typedef int*    IntPtr;
typedef IntPtr* IntPtrPtr;

void main(void) {
    int       x;
    int       y;
    IntPtr    p;
    IntPtrPtr pp;
    void*     vp;
    void*     vpp;
    int*      p2;
    int**     pp2;

    x  = 42;
    y  = 99;
    p  = &x;
    pp = &p;

    /* int* → void* → int*  round-trip */
    vp = p;
    p2 = vp;
    output(*p2);       /* 42 */

    /* int** → void* → int**  round-trip */
    vpp = pp;
    pp2 = vpp;
    output(**pp2);     /* 42 */

    /* modify through the round-tripped pointer-to-pointer */
    *pp2 = &y;         /* p now points to y */
    output(*p);        /* 99 */
    output(**pp2);     /* 99 */
}
