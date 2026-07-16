/* Pulls BOTH archive members (via from_a and from_b), so dup_helper is
 * defined twice among pulled members — the link must succeed. */

long from_a(void);
long from_b(void);

void main(void) {
    output((int)(from_a() + from_b()));   /* 10+x + 20+x for one winner x */
}
