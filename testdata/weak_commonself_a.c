/* Adversary gap (round 3, finding 2): COMMON-beats-weak, losing TU reads
 * its own weak def. Same fileSymVA interposition mechanism as the
 * strong-winner case: A's read must see the winning COMMON (0), not A's
 * dead .data copy (5). */
__attribute__((weak)) int cval = 5;

int getcv(void) { return cval; }
