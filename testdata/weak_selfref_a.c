/* Adversary gap (round 2, finding 1): the losing TU READS its own weak def.
 * fileSymVA resolves same-TU references section-locally with no precedence
 * check, so A's internal read of selfv must still see the strong winner from
 * B — the canonical "library ships weak default, user overrides" shape. */
__attribute__((weak)) int selfv = 1;

int getself(void) { return selfv; }
