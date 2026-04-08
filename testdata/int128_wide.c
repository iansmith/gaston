/* int128_wide.c — exercises all __int128 / __uint128_t operations with
 * nonzero hi words throughout.  Every check that passes outputs 1;
 * arithmetic outputs are labelled in comments.
 *
 * Key values:
 *   max64 = 2^64 - 1  (hi=0, lo=0xFFFF...F)
 *   two64 = 2^64      (hi=1, lo=0)  — built via add-with-carry
 */
void main(void) {
    __uint128_t max64, two64, x;
    __int128 sx;
    unsigned long hi, lo;

    max64 = (__uint128_t)0xFFFFFFFFFFFFFFFFUL;

    /* ── Add with carry: max64 + 1 → hi=1, lo=0 ─────────────────────── */
    two64 = max64 + 1;
    hi = (unsigned long)(two64 >> 64);
    lo = (unsigned long)two64;
    output((int)hi);                              /* 1 */
    output((int)lo);                              /* 0 */

    /* ── Sub with borrow: two64 - 1 → hi=0, lo=0xFFFF...F ───────────── */
    x = two64 - 1;
    hi = (unsigned long)(x >> 64);
    lo = (unsigned long)x;
    output((int)hi);                              /* 0 */
    output((int)(lo == 0xFFFFFFFFFFFFFFFFUL));    /* 1 */

    /* ── Bitwise OR: max64 | two64 → hi=1, lo=0xFFFF...F ────────────── */
    x = max64 | two64;
    hi = (unsigned long)(x >> 64);
    lo = (unsigned long)x;
    output((int)hi);                              /* 1 */
    output((int)(lo == 0xFFFFFFFFFFFFFFFFUL));    /* 1 */

    /* ── Bitwise AND: max64 & two64 → 0 (no overlapping bits) ────────── */
    output((int)((max64 & two64) == 0));          /* 1 */

    /* ── Bitwise XOR: max64 ^ two64 == max64 | two64 (no overlap) ─────── */
    output((int)((max64 ^ two64) == (max64 | two64)));  /* 1 */

    /* ── Left shift crossing boundary: max64 << 1 ────────────────────── */
    /* max64 = 0x0000...0_FFFF...F; << 1 → hi=1, lo=0xFFFF...E        */
    x = max64 << 1;
    hi = (unsigned long)(x >> 64);
    lo = (unsigned long)x;
    output((int)hi);                              /* 1 */
    output((int)lo);                              /* -2 (low 32 bits of 0xFFFF...FE) */

    /* ── Logical right shift: two64 >> 1 → hi=0, lo=2^63 ──────────────── */
    x = two64 >> 1;
    hi = (unsigned long)(x >> 64);
    output((int)hi);                              /* 0 */
    output((int)((x + x) == two64));              /* 1: 2*(two64/2) == two64 */

    /* ── Multiply: 2^32 * 2^32 = 2^64 == two64 ──────────────────────── */
    x = (__uint128_t)0x100000000UL * (__uint128_t)0x100000000UL;
    output((int)(x == two64));                    /* 1 */

    /* ── Unsigned comparisons: hi word must dominate ──────────────────── */
    output((int)(two64 >  max64));                /* 1 */
    output((int)(max64 <  two64));                /* 1 */
    output((int)(two64 >= two64));                /* 1 */
    output((int)(max64 <= max64));                /* 1 */
    output((int)(two64 != max64));                /* 1 */

    /* ── Signed: negation gives nonzero hi word ───────────────────────── */
    /* -two64 in __int128: hi=0xFFFF...F (== -1 as signed long), lo=0   */
    sx = -(__int128)two64;
    output((int)(sx < (__int128)0));              /* 1: negative */
    hi = (unsigned long)((__uint128_t)sx >> 64);
    output((int)(hi == 0xFFFFFFFFFFFFFFFFUL));    /* 1: hi word is all-ones */
    lo  = (unsigned long)sx;
    output((int)(lo == 0));                       /* 1: lo word is zero */

    /* ── Signed comparison: hi word carries the sign ──────────────────── */
    output((int)((__int128)(-1) < (__int128)1));  /* 1 */
    output((int)((__int128)0    < (__int128)two64)); /* 1 */

    /* ── Arithmetic right shift of negative: result stays negative ─────── */
    /* sx = -(2^64) = hi:0xFFFF...F, lo:0; >> 1 → hi:0xFFFF...F, lo:2^63 */
    sx = sx >> 1;
    output((int)(sx < (__int128)0));              /* 1: still negative */
    /* also verify the shift actually moved bits: lo word is now 2^63     */
    lo = (unsigned long)sx;
    output((int)((lo >> 63) == 1));               /* 1 */

    /* ── Negation round-trip: -(-two64) == two64 ────────────────────── */
    output((int)((-(-(__int128)two64)) == (__int128)two64));  /* 1 */
}
