/* See weak_common_big_a.c. COMMON int bigv (4 bytes) vs A's weak long
 * (8 bytes); guard is adjacent COMMON storage read BEFORE main writes it,
 * so a link/load-time clobber (the discarded weak initializer spilling)
 * is observable rather than masked by the test's own store. */
int bigv;
int guard;

void main(void) {
    output(bigv);  /* 0 — COMMON wins over the larger weak def */
    output(guard); /* 0 — pre-write read: corruption detector */
    guard = 9;
    output(guard); /* 9 — slot still writable/readable */
}
