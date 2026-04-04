/* int_promote_arith.cm — integer promotion in cross-type arithmetic (item 8).
   Demonstrates the KEY property of promotion: operands are widened to 64-bit BEFORE
   the operation, so there is no overflow at char/short width during the computation.
   Only when the result is assigned back to a narrow type does truncation occur. */
void main(void) {
    char c;
    short s;
    int result;

    /* char(127) + short(127): each widened to int before add → 254
       There is no 8-bit or 16-bit intermediate overflow. */
    c = 127;
    s = 127;
    result = c + s;
    output(result);   /* 254 */

    /* Assign 254 back to char: 254 in two's complement 8-bit = -2 (sign bit set) */
    c = result;
    output(c);        /* -2 */

    /* char(100) * short(300): widened to int → 30000
       Would be 100*300 mod 256 = 140 as unsigned char, but promotion prevents that. */
    c = 100;
    s = 300;
    result = c * s;
    output(result);   /* 30000 */

    /* Assign 30000 back to short: 30000 < 32768 so no truncation */
    s = result;
    output(s);        /* 30000 */

    /* char(-1 = 255 unsigned) + short(1): -1 widened to int = -1, result = 0 */
    c = 255;          /* stored as -1 in signed char */
    s = 1;
    result = c + s;
    output(result);   /* 0 */
}
