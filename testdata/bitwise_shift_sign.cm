/* bitwise_shift_sign.cm — signed vs unsigned right shift behavior.
   Arithmetic right shift (>>, signed) propagates the sign bit.
   Logical right shift (>>, unsigned) fills with zeros from the top.

   Key values:
     -8  = 0xFFFFFFFFFFFFFFF8  (signed int)
     -1  = 0xFFFFFFFFFFFFFFFF
     -128 = 0xFFFFFFFFFFFFFF80

   Signed (arithmetic) >> :
     -8 >> 1  = -4            (sign bit propagated)
     -8 >> 2  = -2
     -1 >> 30 = -1            (all 1s → still all 1s)
     -128 >> 3 = -16

   Unsigned long (logical) >> :
     (unsigned long)-8  >> 1  = very large positive
     (unsigned long)-8  >> 62 = 3   (top 2 bits of 0xFF...FF...F8 are 11 = 3)
     (unsigned long)-1  >> 1  = 2^63 - 1 ... too large for output; use smaller
     (unsigned long)256 >> 1  = 128  (positive, same as signed)

   <<  (left shift, same for signed/unsigned):
     1  << 0  = 1
     1  << 7  = 128
     1  << 16 = 65536
     -1 << 0  = -1  (no change)

   Expected output:
   -4      (signed -8 >> 1)
   -2      (signed -8 >> 2)
   -1      (signed -1 >> 30)
   -16     (signed -128 >> 3)
   1       (signed 2 >> 0 — shift by zero)
   3       (unsigned (-8) >> 62 — logical, top 2 bits)
   2147483647  (unsigned 0x7FFFFFFF >> 0 — same as signed, large positive)
   128     (unsigned 256 >> 1)
   1       (1 << 0)
   128     (1 << 7)
   65536   (1 << 16)
   -1      (-1 << 0)
   -2      (-1 << 0 then -1 via compound: test <<= with var shift)
   8       (1 <<= 3 compound)
   2       (8 >>= 2 compound, signed)
   1073741824 (1 << 30: largest safe positive int shift)
*/

void main(void) {
    int   s;
    unsigned long u;

    /* ── signed arithmetic right shift: sign bit propagates ─ */
    s = -8;
    output(s >> 1);     /* -4  */
    output(s >> 2);     /* -2  */

    s = -1;
    output(s >> 30);    /* -1: 0xFF..FF >> 30 = 0xFF..FF = -1 */

    s = -128;
    output(s >> 3);     /* -16: -128/8 = -16 */

    s = 2;
    output(s >> 0);     /* 2: shift by zero is identity */

    /* ── unsigned logical right shift: zeros fill from top ── */
    u = -8;             /* 0xFFFFFFFFFFFFFFF8 as unsigned */
    output(u >> 62);    /* 3: top 2 bits of -8 are 11 */

    u = 2147483647;     /* 0x7FFFFFFF */
    output(u >> 0);     /* 2147483647: identity */

    u = 256;
    output(u >> 1);     /* 128 */

    /* ── left shift ──────────────────────────────────────────── */
    s = 1;
    output(s << 0);     /* 1   */
    output(s << 7);     /* 128 */
    output(s << 16);    /* 65536 */

    s = -1;
    output(s << 0);     /* -1 */

    /* ── compound shift assignments ────────────────────────── */
    s = 1;
    s <<= 3;
    output(s);          /* 8 */

    s >>= 2;            /* signed: 8 >> 2 = 2 */
    output(s);          /* 2 */

    s = 1;
    output(s << 30);    /* 1073741824 */
}
