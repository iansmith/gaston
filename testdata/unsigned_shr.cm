/* unsigned right shift — LSR must be used, not ASR.
   If ASR is used, shifting a negative value propagates the sign bit.
   If LSR is used, zeros shift in from the left.

   -8 = 0xFFFFFFFFFFFFFFF8
   LSR >>62: top 2 bits 11 → result = 3
   ASR >>62: sign-extended → result = -1

   -4 = 0xFFFFFFFFFFFFFFFC
   LSR >>1: 0x7FFFFFFFFFFFFFFE → positive, > 0
   ASR >>1: 0xFFFFFFFFFFFFFFFE = -2 → negative, < 0 (signed)    */
int main(void) {
    unsigned int a;
    unsigned int b;

    a = -8;
    output(a >> 62);    /* LSR: 3   |  ASR: -1 */

    b = -4;
    /* (b >> 1) is unsigned, so compare uses unsigned condition */
    if (b >> 1 > 0) {
        output(1);      /* correct: LSR shifts in 0, result > 0 unsigned */
    } else {
        output(0);      /* wrong: ASR would keep top bit set */
    }
    return 0;
}
