/* Adversary gap (round 1, finding 3): a weak def in an already-linked user
 * object SATISFIES references per gABI — an archive member holding a strong
 * def of the same name must NOT be auto-pulled. */
__attribute__((weak)) int gsup = 5;
