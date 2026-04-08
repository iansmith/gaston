/* err_double_ptr_inner.cm — both sides are "double pointers" (**), but the
   innermost element type differs (int vs char).  A naive depth check would
   accept this; the parameterised CType must recurse all the way to the leaf. */

void main(void) {
    int   x;
    char  c;
    int*  ip;
    char* cp;
    int** ipp;
    char** cpp;

    ip  = &x;
    cp  = &c;
    ipp = &ip;
    cpp = &cp;

    ipp = cpp;   /* ERROR: int** != char** (inner type differs) */
}
