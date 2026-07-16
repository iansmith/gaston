/* Cross-TU tentative definitions, TU A.
 * `both` is tentatively defined in BOTH TUs — the linker must merge them into
 * one storage slot. `shared` is tentative here but has a real (initialized)
 * definition in TU B — the real definition must win. */

long both;      /* tentative here AND in tentative_multi_b.c */
long shared;    /* tentative here; real def (= 42) in tentative_multi_b.c */

long b_read_both(void);

void main(void) {
    output((int)shared);        /* 42 — real def in B wins over tentative in A */
    both = 9;
    output((int)b_read_both()); /* 9 — B sees A's write: one merged slot */
}
