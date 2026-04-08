/* bitwise_byzantine.cm — comprehensive bitwise stress test.
   Combines: &, |, ^, ~, <<, >> with mixed types (int, short, char, unsigned),
   explicit casts, compound assignments, signed/unsigned shift semantics.

   All expected values computed from 64-bit arithmetic (gaston local vars are 64-bit):

   Section A — bitwise on char/short variables (promotion to int before op):
     char c = -1 = 0xFF (stored as 64-bit -1, read with SXTB → -1 promoted to int)
     ~c (after sign-extend) = ~(-1) = 0
     c & 0x0F = -1 & 15 = 15
     short s = -256 = 0xFF00 (16-bit pattern)
     s | 0x00FF = -256 | 255 = -1 (all 16 low bits set)
     s ^ 0xFFFF = -256 ^ 65535: integer promotion extends s to 64-bit first.
       -256 promoted = 0xFFFFFFFFFFFFFF00. 65535 = 0x000000000000FFFF.
       XOR = 0xFFFFFFFFFFFF00FF = -65281. (NOT 255 — promotion matters!)
       To get just the 16-bit flip: (short)(s ^ 0xFFFF) = 255.

   Section B — casts and bitwise:
     (unsigned char)(-1) & 0x0F = 255 & 15 = 15
     (short)0xABCD & 0xFF:
       0xABCD = 43981. (short)43981: lower 16 = 0xABCD, sign-ext = -21555.
       -21555 & 0xFF: -21555 & 255. -21555 = 0xFF...FFABCD... wait.
       Actually (short)43981: 43981 & 0xFFFF = 0xABCD = 43981. But 0xABCD > 0x7FFF (MSB=1):
       43981 - 65536 = -21555. So (short)43981 = -21555.
       -21555 & 0xFF: -21555 = 0xFFFFFFFFFFFFABCD. Lower 8 = 0xCD = 205. signed = -51.
       Wait: 0xCD as signed byte = 205. But we're doing & 0xFF on an int (not a byte).
       -21555 & 0xFF: 0xFF = 255. -21555 & 255 = 0xCD & 0xFF = 0xCD = 205 (no sign extension since & 0xFF zeroes upper bits). output = 205.
     (unsigned char)0xABCD: lower 8 = 0xCD = 205. zero-extended = 205.
     205 & 0xFF = 205. ✓

   Section C — XOR tricks:
     XOR swap: a=10, b=20. a^=b; b^=a; a^=b. → a=20, b=10.
     XOR toggle bit: flags = 0b10110101 = 181.
       flags ^= 0b00000100 = 4. → 181^4 = 177 (bit 2 cleared, was set). 0b10110001 = 177.
       flags ^= 4 again → 181 (restored).

   Section D — shift with casts:
     (char)(1 << 7): 1<<7 = 128. (char)128 = -128 (0x80 sign-extended to 8 bits).
     (unsigned char)(1 << 7): 128. zero-extended stays 128.
     (short)(1 << 15): 1<<15=32768. (short)32768: 32768 & 0xFFFF = 0x8000 sign-ext = -32768.
     (int)((unsigned int)-1 >> 1): logical shift: 0xFF...FF >> 1 = 0x7FF...FF = 9223372036854775807.
       But output is signed, and that's a very large number. Let me use a smaller value.
       (unsigned int)256 >> 3 = 32. ✓

   Section E — compound &=, |=, ^=, <<=, >>= on different types:
     short s = 0xFF; s &= 0x0F; → s = 15. output(s) = 15.
     char c = 60; c |= 3; → c = 60|3 = 63. output(c) = 63.
     int x = 0xABCDEF; x ^= 0xFF0000; → 0xABCDEF ^ 0xFF0000 = 0x5400CDEF... wait:
       0xABCDEF = 11259375. 0xFF0000 = 16711680.
       11259375 ^ 16711680: let's compute bitwise.
       0x00ABCDEF ^ 0x00FF0000 = 0x00543DEF = 5520879.  Wait:
       AB ^ FF = 54, CD stays, EF stays → 0x54CDEF. Hmm:
       0xAB ^ 0xFF = 0xAB XOR 0xFF:
         AB = 10101011, FF = 11111111, XOR = 01010100 = 0x54. ✓
       So 0x00ABCDEF ^ 0x00FF0000 = 0x0054CDEF = 5526991.
       5526991 = 0x0054CDEF. ✓ Let me verify: 0x54=84, 0xCD=205, 0xEF=239.
       84*65536 + 205*256 + 239 = 5505024 + 52480 + 239 = 5557743. Hmm.
       Let me just compute 11259375 ^ 16711680 directly:
       11259375 = 0xABCDEF, 16711680 = 0xFF0000.
       Bit by bit: 0xABCDEF = 0x00ABCDEF. 0xFF0000. XOR:
       byte 2 (3rd byte from right): 0xAB ^ 0xFF = 0x54.
       byte 1: 0xCD ^ 0x00 = 0xCD.
       byte 0: 0xEF ^ 0x00 = 0xEF.
       byte 3+: 0x00 ^ 0x00 = 0x00.
       Result: 0x0054CDEF. = 5 * 2^20 + ... let me compute: 0x0054CDEF.
       0x54 = 84. 84*65536 = 5505024. 0xCD = 205. 205*256 = 52480. 0xEF = 239.
       Total: 5505024 + 52480 + 239 = 5557743.
       So x ^= 0xFF0000 → 5557743.
     int y = 255; y <<= 8; → 255*256 = 65280. then y >>= 4 → 65280/16 = 4080.

   Expected output:
   0        (~c with c=-1: ~(-1)=0)
   15       (c & 15 with c=-1: -1 & 15 = 15)
   -1       (s | 255 with s=-256: -256|255 = -1)
   -65281   (s ^ 65535 with s=-256: promoted to 64-bit, -256^65535 = -65281)
   15       ((unsigned char)(-1) & 15 = 255 & 15)
   205      ((short)43981 & 255 = -21555 & 255 = 0xCD = 205)
   20       (XOR swap: a after)
   10       (XOR swap: b after)
   177      (flags ^= 4: 181^4 = 177)
   181      (flags ^= 4 again: restored)
   -128     ((char)(1<<7): 128 as signed byte)
   128      ((unsigned char)(1<<7): 128 unsigned)
   -32768   ((short)(1<<15))
   32       (256u >> 3)
   15       (short s=255; s&=15 → 15)
   63       (char c=60; c|=3 → 63)
   5557743  (int x=0xABCDEF; x^=0xFF0000)
   65280    (int y=255; y<<=8)
   4080     (y>>=4 → 65280/16=4080)
*/

int xor_bits(int a, int b) {
    return a ^ b;
}

void main(void) {
    int   x;
    int   y;
    short s;
    char  c;
    unsigned int  u;
    unsigned char uc;
    int   flags;
    int   a;
    int   b;

    /* ── Section A: bitwise on promoted char/short ────────── */
    c = -1;                     /* 0xFF, sign-extends to -1 */
    output(~c);                 /* ~(-1) = 0                */
    output(c & 15);             /* -1 & 0x0F = 15           */

    s = -256;                   /* 0xFF00 pattern in 16 bits */
    output(s | 255);            /* -256 | 255 = -1          */
    output(s ^ 65535);          /* -256 ^ 65535 = 255       */

    /* ── Section B: casts combined with bitwise ─────────────── */
    output((unsigned char)(-1) & 15); /* 255 & 15 = 15         */
    output((short)43981 & 255);       /* -21555 & 255 = 0xCD = 205 */

    /* ── Section C: XOR tricks ───────────────────────────────── */
    /* XOR swap (no temp variable) */
    a = 10;
    b = 20;
    a ^= b;
    b ^= a;
    a ^= b;
    output(a);   /* 20 */
    output(b);   /* 10 */

    /* XOR bit toggle */
    flags = 181;             /* 0b10110101 = 181 */
    flags ^= 4;              /* clear bit 2: 181^4 = 177 */
    output(flags);           /* 177 */
    flags ^= 4;              /* restore bit 2: 177^4 = 181 */
    output(flags);           /* 181 */

    /* ── Section D: shifts with casts ──────────────────────── */
    output((char)(1 << 7));           /* -128: 128 sign-extended to 8 bits */
    output((unsigned char)(1 << 7)); /* 128: zero-extended                 */
    output((short)(1 << 15));         /* -32768: 32768 sign-ext to 16 bits */

    u = 256;
    output(u >> 3);                   /* 32: unsigned logical shift        */

    /* ── Section E: compound bitwise assignments ─────────────── */
    s = 255;
    s &= 15;
    output(s);   /* 15: 255 & 15 = 0x0F */

    c = 60;
    c |= 3;
    output(c);   /* 63: 60 | 3 = 0x3F */

    x = 11259375;  /* 0xABCDEF */
    x ^= 16711680; /* 0xFF0000: flip byte 2 */
    output(x);     /* 5557743: 0x0054CDEF */

    y = 255;
    y <<= 8;
    output(y);   /* 65280: 255 * 256 */

    y >>= 4;     /* 65280 >> 4 = 4080 */
    output(y);   /* 4080 */
}
