/* Archive-duplicate tolerance test, member B: also defines dup_helper(). */

long dup_helper(void) {
    return 2;
}

long from_b(void) {
    return 20 + dup_helper();
}
