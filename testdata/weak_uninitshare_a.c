/* Adversary gap (round 3, finding 1): weak .bss STORAGE IDENTITY. The
 * uninitialized weak def must be ONE shared slot — a per-TU-duplicated
 * zeroed slot reads 0 correctly but loses writes made from another TU. */
__attribute__((weak)) int zshare;

int getz(void) { return zshare; }
