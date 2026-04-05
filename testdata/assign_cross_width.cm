/* assign_cross_width.cm — cross-width integer assignments between int, short, char.
   Tests both narrowing (wide → narrow) and widening (narrow → wide) assignments.
   Truncation happens at load-time via sign/zero extension (SXTB/SXTH/UXTB/UXTH).

   LP64 bit patterns used:
     100000 = 0x000186A0: lower 16 = 0x86A0 = 34464 → signed short -31072
                           lower  8 = 0xA0 = 160   → signed char  -96
     70000  = 0x00011170: lower 16 = 0x1170 = 4464 → signed short +4464
     -500   = 0xFFFFFE0C: lower  8 = 0x0C = 12     → signed char  +12
     -100   = 0xFFFFFF9C                             → signed char  -100
     60000  = 0x0000EA60                             → unsigned short 60000
     200    = 0xC8                                   → unsigned char  200

   Expected output (one value per line):
   -31072   (int 100000 → short, negative truncation)
   34464    (int 100000 → unsigned short, bit-identical, unsigned interpretation)
   -96      (int 100000 → char, negative truncation)
   4464     (long 70000 → short, positive truncation)
   12       (short -500 → char, positive result from lower byte)
   -100     (char -100 → int, sign extension)
   -100     (char -100 → long, sign extension)
   -100     (char -100 → short, sign extension)
   200      (unsigned char 200 → int, zero extension)
   200      (unsigned char 200 → short, zero extension)
   -500     (short -500 → int, sign extension)
   60000    (unsigned short 60000 → int, zero extension)
*/

void main(void) {
    int i;
    long l;
    short s;
    unsigned short us;
    char c;
    unsigned char uc;

    /* ── narrowing: int → short ─────────────────────────── */
    i = 100000;
    s = i;
    output(s);        /* -31072: 0x86A0 sign-extended from 16 bits */

    /* ── narrowing: int → unsigned short ───────────────── */
    i = 100000;
    us = i;
    output(us);       /* 34464: 0x86A0 zero-extended from 16 bits */

    /* ── narrowing: int → char ──────────────────────────── */
    i = 100000;
    c = i;
    output(c);        /* -96: 0xA0 sign-extended from 8 bits */

    /* ── narrowing: long → short (long = int in gaston) ─── */
    l = 70000;
    s = l;
    output(s);        /* 4464: 0x1170 sign-extended from 16 bits */

    /* ── narrowing: short → char ─────────────────────────── */
    s = -500;
    c = s;
    output(c);        /* 12: lower byte of -500 = 0x0C, positive char */

    /* ── widening: char → int, sign extension ─────────────── */
    c = -100;
    i = c;
    output(i);        /* -100 */

    /* ── widening: char → long, sign extension ──────────── */
    c = -100;
    l = c;
    output(l);        /* -100 */

    /* ── widening: char → short, sign extension ─────────── */
    c = -100;
    s = c;
    output(s);        /* -100: sign-extended to 16 bits, still -100 */

    /* ── widening: unsigned char → int, zero extension ────── */
    uc = 200;
    i = uc;
    output(i);        /* 200: 0xC8 zero-extended */

    /* ── widening: unsigned char → short, zero extension ─── */
    uc = 200;
    s = uc;
    output(s);        /* 200: fits in short, stays positive */

    /* ── widening: short → int, sign extension ──────────── */
    s = -500;
    i = s;
    output(i);        /* -500 */

    /* ── widening: unsigned short → int, zero extension ─── */
    us = 60000;
    i = us;
    output(i);        /* 60000: 0xEA60 zero-extended, positive */
}
