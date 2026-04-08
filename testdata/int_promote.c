/* int_promote.cm — integer promotion: char/short widen to int before arithmetic.
   All results are output as int (64-bit signed) so we see the promoted value. */

void main(void) {
    char c;
    unsigned char uc;
    short s;
    unsigned short us;

    /* signed char overflow: 127+1 wraps to -128 in C */
    c = 127;
    c = c + 1;
    output(c);           /* -128 */

    /* signed char negative: stored as 200, promoted to -56 */
    c = 200;
    output(c);           /* -56 */

    /* signed char compound assign: 200++ = -55 */
    c = 200;
    c++;
    output(c);           /* -55 */

    /* unsigned char: 200 stays 200 */
    uc = 200;
    output(uc);          /* 200 */

    /* unsigned char overflow: 255+1 = 0 */
    uc = 255;
    uc = uc + 1;
    output(uc);          /* 0 */

    /* short: 32767+1 wraps to -32768 */
    s = 32767;
    s = s + 1;
    output(s);           /* -32768 */

    /* unsigned short: 65535+1 wraps to 0 */
    us = 65535;
    us = us + 1;
    output(us);          /* 0 */
}
