/* Adversary gap (round 1, finding 1): weak function vs strong function of
 * the same name — strong wins, no duplicate-definition error. Precedence
 * pin (green pre-fix: today the weak def is dropped entirely); guards
 * against a naive union-STB_WEAK-into-STB_GLOBAL fix erroring or inverting. */
__attribute__((weak)) int pick(void) { return 1; }
