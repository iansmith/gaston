/* short and unsigned short — stored as 64-bit in frame (same as char).
   Tests all keyword forms and basic arithmetic. */
int main(void) {
    short a;
    short int b;
    unsigned short c;
    unsigned short int d;

    a = 100;
    b = 200;
    c = 300;
    d = 400;

    output(a + b + c + d);   /* 1000 */
    output(a * b);            /* 20000 */
    output(b - a);            /* 100 */

    /* short compound assignments */
    a += 50;   output(a);   /* 150 */
    a /= 3;    output(a);   /* 50  */

    /* unsigned short — same bit operations */
    c += d;    output(c);   /* 700 */

    return 0;
}
