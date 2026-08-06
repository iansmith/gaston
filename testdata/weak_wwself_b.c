/* See weak_wwself_a.c. B's weak def loses (linked second); B's own read of
 * wv must see the winning first def (11), not its dead copy (22). This is
 * the tie case where precedence-conditional interposition ("redirect iff a
 * HIGHER-precedence def exists") diverges from winner-identity
 * interposition (gABI: all references resolve to the one chosen def). */
__attribute__((weak)) int wv = 22;

int getwv(void) { return wv; }

void main(void) {
    output(getwv()); /* 11 — B reads the winner, not its own dead copy */
}
