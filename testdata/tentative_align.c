/* Alignment of tentative definitions must honor each symbol's natural
 * alignment regardless of layout order: p(16) q(8) r(16) — under both
 * declaration order and alphabetical order, r needs padding to stay
 * 16-aligned, so an alignment-ignoring allocator fails. */

long double p;
long q;
long double r;
double d8;

void main(void) {
    output((int)((long)&p & 15));   /* 0 */
    output((int)((long)&r & 15));   /* 0 */
    output((int)((long)&d8 & 7));   /* 0 */
}
