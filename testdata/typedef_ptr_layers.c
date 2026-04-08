/* typedef_ptr_layers.cm — struct Point* via layered typedefs.
   PointPtr = struct Point*,  PointPtrPtr = struct Point**.
   Tests that typedef'd pointer types carry the correct CType through
   function parameters, double-deref, and arrow-through-deref. */

struct Point { int x; int y; };

typedef struct Point*  PointPtr;
typedef PointPtr*      PointPtrPtr;

int psum(PointPtr p) {
    return p->x + p->y;
}

void pset(PointPtrPtr pp, int nx, int ny) {
    /* deref pp to get the PointPtr, then mutate through it */
    PointPtr cur;
    cur = *pp;
    cur->x = nx;
    cur->y = ny;
}

void main(void) {
    struct Point a;
    struct Point b;
    PointPtr     pa;
    PointPtr     pb;
    PointPtrPtr  ppa;

    a.x = 3;  a.y = 4;
    b.x = 10; b.y = 5;
    pa  = &a;
    pb  = &b;
    ppa = &pa;

    output(psum(pa));        /* 7 */
    output(psum(*ppa));      /* 7 — deref PointPtrPtr → PointPtr */

    pset(ppa, 100, 200);
    output(psum(*ppa));      /* 300 — struct mutated through ** */
    output(a.x + a.y);      /* 300 — same struct, direct access */

    /* redirect ppa to point at pb */
    *ppa = pb;
    output(psum(*ppa));      /* 15 */

    pset(ppa, 1, 2);
    output(psum(*ppa));      /* 3 */
    output(b.x + b.y);      /* 3 */
}
