/* C11 6.9.2p2 within one TU: a tentative definition followed by a real
 * definition of the same identifier is ONE real definition; two tentative
 * definitions are ONE tentative definition. */

long x;
long x = 3;
long y;
long y;

void main(void) {
    output((int)x);   /* 3 */
    y = 4;
    output((int)y);   /* 4 */
}
