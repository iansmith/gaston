/* sizeof_types.cm — sizeof for all scalar types beyond int/char/pointer.
   Tests items 2 and 5: all sizes match LP64 C ABI.
   float=4, double=8, short=2, unsigned short=2, unsigned char=1, int=4, char=1. */
void main(void) {
    output(sizeof(float));          /* 4 — float is 32-bit per C ABI */
    output(sizeof(double));         /* 8 — double is 64-bit */
    output(sizeof(short));          /* 2 — short is 16-bit per C ABI */
    output(sizeof(unsigned short)); /* 2 — unsigned short is also 16-bit */
    output(sizeof(unsigned char));  /* 1 — unsigned char is 8-bit */
    output(sizeof(int));            /* 4 — int is 32-bit per LP64 C ABI */
    output(sizeof(char));           /* 1 — char is 8-bit */
}
