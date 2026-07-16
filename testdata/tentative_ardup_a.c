/* Archive-duplicate tolerance test, member A: defines its own entry point
 * plus dup_helper(), which member B ALSO defines. Real archives ship such
 * duplicates (e.g. __isnand in both mathbuiltins and libm); pulling both
 * members must link with a warning (last definition wins), not hard-error.
 * Duplicate definitions across explicit user objects remain an error. */

long dup_helper(void) {
    return 1;
}

long from_a(void) {
    return 10 + dup_helper();
}
