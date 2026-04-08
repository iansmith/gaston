/* err_typedef_chain_depth.cm — the most convoluted path.
   T2 and U2 are both "double-pointer" typedefs, but T2 resolves to int**
   while U2 resolves to char**.  To catch this the checker must follow:
     sp = pp
     → sp is U2  → lookupTypedef U2 → U1* → lookupTypedef U1 → char* → char**
     → pp is T2  → lookupTypedef T2 → T1* → lookupTypedef T1 → int*  → int**
   then recurse into the pointee CTypes to find int != char at the leaf. */

typedef int*  T1;   /* T1 = int*  */
typedef T1*   T2;   /* T2 = int** */
typedef char* U1;   /* U1 = char* */
typedef U1*   U2;   /* U2 = char** */

void main(void) {
    int x;
    T1  p;
    T2  pp;
    U2  sp;

    p  = &x;
    pp = &p;      /* T2 = &T1  ✓  (int** = &(int*)) */
    sp = pp;      /* ERROR: T2(=int**) != U2(=char**) */
}
