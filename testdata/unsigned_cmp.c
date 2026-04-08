/* unsigned comparison — the key test is that a value that is negative
   when interpreted as signed compares as GREATER THAN a small positive
   value when using unsigned comparison.

   -1 stored in an unsigned int = 0xFFFFFFFFFFFFFFFF = 18446744073709551615
   Signed:   -1 < 1  → comparison should give 0
   Unsigned: UINT_MAX > 1 → comparison should give 1              */
int main(void) {
    unsigned int big;
    unsigned int small;
    big = -1;       /* all bits set: largest unsigned value */
    small = 1;

    /* These should all be 1 (true) for unsigned, 0 (false) for signed */
    if (big > small)  { output(1); } else { output(0); }
    if (big >= small) { output(1); } else { output(0); }
    if (small < big)  { output(1); } else { output(0); }
    if (small <= big) { output(1); } else { output(0); }

    /* Signed comparisons of the same values would give wrong results.
       Verify == and != are still correct (sign-independent). */
    if (big == big)   { output(1); } else { output(0); }
    if (big != small) { output(1); } else { output(0); }
    return 0;
}
