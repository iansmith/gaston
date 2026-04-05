/* err_typedef_mismatch.cm — typedef names hide incompatible base types.
   IntPtr and CharPtr look like two opaque "pointer" names, but they resolve
   to int* and char* respectively.  Assigning one to the other must be caught
   even though neither name contains the word "int" or "char" near the site
   of the offending assignment. */

typedef int*  IntPtr;
typedef char* CharPtr;

void main(void) {
    int    x;
    IntPtr  ip;
    CharPtr cp;
    ip = &x;
    cp = ip;   /* ERROR: IntPtr(=int*) != CharPtr(=char*) */
}
