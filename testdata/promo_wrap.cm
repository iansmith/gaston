/* promo_wrap.cm — integer promotion prevents intermediate overflow; wrap-around
   happens only on assignment back to the narrow type.
   Byzantine test for item 8: char+char → int (200, no overflow), then stored
   back to char (→ -56 via SXTB); cross-type short*char multiplication.

   Expected: 200 -56 10000 -32768 */

void main(void) {
    char  a;
    char  b;
    char  c;
    short s;

    a = 100;
    b = 100;
    s = a + b;          /* 100+100=200 as int; 200 fits in short */
    output(s);          /* 200 */

    c = a + b;          /* 200 stored into char slot; SXTB(0xC8) = -56 */
    output(c);          /* -56 */

    s = 200;
    a = 50;
    output(s * a);      /* SXTH(200)*SXTB(50) = 200*50 = 10000; no overflow */

    s = 32767;
    s = s + 1;          /* 32768 stored in short slot; SXTH(0x8000) = -32768 */
    output(s);          /* -32768 */
}
