/* cast_cross_width.cm — explicit casts between integer widths: int, short, char, unsigned.
   Tests (short), (unsigned short), (char), (unsigned char) casts from wider types,
   and multi-step cast chains.

   Bit patterns:
     100000 = 0x000186A0: lower 16 = 0x86A0 = 34464 → (short) -31072, (unsigned short) 34464
                           lower  8 = 0xA0 = 160    → (char)  -96,    (unsigned char) 160
     -1 (int):             lower 16 = 0xFFFF         → (unsigned short) 65535
                           lower  8 = 0xFF            → (unsigned char) 255
     300:                  lower  8 = 0x2C = 44       → (char) 44
     -100:                 lower  8 = 0x9C = -100     → (char) -100
                           lower 16 from (char)(-100) = 0xFF9C = 65436 → (unsigned short) 65436
     200:                  (unsigned char) 200, then (int) = 200

   Expected output (one value per line):
   -31072    (short)100000
   34464     (unsigned short)100000
   -96       (char)100000
   160       (unsigned char)100000
   -1        (short)(-1)         — fits: -1 sign-extended is still -1
   65535     (unsigned short)(-1)
   255       (unsigned char)(-1)
   -31072    (int)(short)100000  — cast chain: short truncation then widen to int
   -96       (char)(short)100000 — lower 8 of -31072 = 0xA0 → -96
   44        (short)(char)300    — (char)300 = 44, then (short) = 44
   34464     (unsigned short)(short)100000 — same 16 bits as -31072, unsigned interpretation
   200       (int)(unsigned char)200
   65436     (unsigned short)(char)(-100) — sign chain: (char)(-100)=-100, (us)(-100)=0xFF9C=65436
*/

void main(void) {
    /* ── simple casts from int to narrow types ──────────── */
    output((short)100000);              /* -31072 */
    output((unsigned short)100000);     /* 34464  */
    output((char)100000);               /* -96    */
    output((unsigned char)100000);      /* 160    */

    /* ── casts from -1: fills all bits ─────────────────── */
    output((short)(-1));                /* -1     */
    output((unsigned short)(-1));       /* 65535  */
    output((unsigned char)(-1));        /* 255    */

    /* ── cast chains ────────────────────────────────────── */

    /* int → short → int: round-trip narrows to 16 bits */
    output((int)(short)100000);         /* -31072 */

    /* int → short → char: cascade narrowing */
    output((char)(short)100000);        /* -96    */

    /* int → char → short: char truncation dominates */
    output((short)(char)300);           /* 44     */

    /* int → short, reinterpret as unsigned short */
    output((unsigned short)(short)100000); /* 34464 */

    /* int → unsigned char → int: zero-extends at char step */
    output((int)(unsigned char)200);    /* 200    */

    /* int → char → unsigned short: sign extension + zero-extend */
    output((unsigned short)(char)(-100)); /* 65436 */
}
