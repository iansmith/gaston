/* bitwise_unit.cm — unit tests for all six bitwise operators: &, |, ^, ~, <<, >>
   Each operator is tested in isolation on known values, then compound-assignment forms.

   Values used:
     0xF0 = 240    0x0F = 15     0xFF = 255    0xAA = 170   0x55 = 85
     0xCC = 204    0x33 = 51

   Expected output (one value per line):
   0        (240 & 15  = 0x00)
   255      (240 | 15  = 0xFF)
   255      (240 ^ 15  = 0xFF)
   -241     (~240      = 0xFFFFFFFFFFFFFF0F in 64-bit signed)
   480      (240 << 1  = 0x1E0)
   120      (240 >> 1  = 0x78, arithmetic, value positive)
   170      (0xAA)
   85       (0x55)
   0        (0xAA & 0x55  = 0x00)
   255      (0xAA | 0x55  = 0xFF)
   255      (0xAA ^ 0x55  = 0xFF)
   0        (0xAA ^ 0xAA  = 0x00  — XOR with self = 0)
   170      (0 ^ 0xAA     = 0xAA  — XOR with 0 = identity)
   51       (0xCC ^ 0xFF  = 0x33  — 204^255=51)
   204      (51 ^ 0xFF    = 0xCC  — inverse)
   10       (&= test: 15 &= 10 → 10)
   11       (|= test: 8  |= 3  → 11)
   9        (^= test: 12 ^= 5  → 9)
   48       (<<= test: 3 <<= 4 → 48)
   3        (>>= test: 48 >>= 4 → 3)
*/

void main(void) {
    int a;
    int b;
    int r;

    /* ── AND ──────────────────────────────────────────────── */
    a = 240;   /* 0xF0 */
    b = 15;    /* 0x0F */
    output(a & b);   /* 0 */

    /* ── OR ───────────────────────────────────────────────── */
    output(a | b);   /* 255 */

    /* ── XOR ──────────────────────────────────────────────── */
    output(a ^ b);   /* 255: 0xF0 ^ 0x0F = 0xFF */

    /* ── NOT ──────────────────────────────────────────────── */
    output(~a);      /* -241: ~240 in 64-bit signed */

    /* ── SHL ──────────────────────────────────────────────── */
    output(a << 1);  /* 480 */

    /* ── SHR (signed, positive value) ────────────────────── */
    output(a >> 1);  /* 120 */

    /* ── XOR properties ───────────────────────────────────── */
    a = 170;   /* 0xAA */
    b = 85;    /* 0x55 */
    output(a);          /* 170 */
    output(b);          /* 85  */
    output(a & b);      /* 0   — no bits in common */
    output(a | b);      /* 255 — all 8 low bits set */
    output(a ^ b);      /* 255 — all bits differ */
    output(a ^ a);      /* 0   — XOR with self */
    output(0 ^ a);      /* 170 — XOR with 0 is identity */

    a = 204;   /* 0xCC */
    output(a ^ 255);    /* 51  = 0x33: flip all 8 bits */
    r = a ^ 255;
    output(r ^ 255);    /* 204 = 0xCC: double-flip restores */

    /* ── compound assignments ─────────────────────────────── */
    a = 15;
    a &= 10;   /* 15 & 10 = 0x0F & 0x0A = 0x0A = 10 */
    output(a); /* 10 */

    a = 8;
    a |= 3;    /* 8 | 3 = 11 */
    output(a); /* 11 */

    a = 12;
    a ^= 5;    /* 12 ^ 5 = 0b1100 ^ 0b0101 = 0b1001 = 9 */
    output(a); /* 9 */

    a = 3;
    a <<= 4;   /* 3 << 4 = 48 */
    output(a); /* 48 */

    a >>= 4;   /* 48 >> 4 = 3 */
    output(a); /* 3 */
}
