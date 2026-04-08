/* charpp_table.cm — char** double indirection via individual char* variables.
   Byzantine test for items 4+12: TypeCharPtrPtr, deref-through-pointer-to-pointer,
   writing through the inner pointer, reading back via outer pointer.

   Uses individual char* variables (not char*[]) to avoid the array-of-pointers
   limitation (item 12 gap). Tests char** re-seat and double-deref.

   Expected: 65 66 90 */

char buf0[4];
char buf1[4];

void main(void) {
    char *p0;
    char *p1;
    char **pp;

    p0 = &buf0;
    p1 = &buf1;

    /* Test char** pointing to p0 */
    pp = &p0;
    *p0 = 65;
    output(**pp);    /* 65 — double-deref through pp → p0 → buf0[0] */

    /* Re-seat pp to p1 */
    pp = &p1;
    *p1 = 66;
    output(**pp);    /* 66 — double-deref through pp → p1 → buf1[0] */

    /* Direct char array write and subscript read */
    buf0[1] = 90;
    output(buf0[1]); /* 90 = 'Z' */
}
