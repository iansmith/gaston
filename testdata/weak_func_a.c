/* Adversary gap (round 1, finding 1): cross-TU weak FUNCTION definition.
 * The symVA .text case sits inside the same STB_GLOBAL filter as the data
 * cases, so a fix that repairs only .data/.bss/COMMON goes green while a
 * cross-TU call to a weak function still branches to VA 0. */
__attribute__((weak)) int get(void) { return 7; }
