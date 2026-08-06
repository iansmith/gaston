/* Adversary gap (round 1, finding 4): weak def LARGER than the COMMON of
 * the same name. COMMON still wins (gABI); the discarded weak def's size
 * must not corrupt slot sizing or a neighboring COMMON. Precedence pin
 * (green pre-fix). */
__attribute__((weak)) long bigv = 5;
