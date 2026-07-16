/* Main for the tentative-vs-archive-real test: this object's tentative
 * definition satisfies g_tent, so the archive member holding the real
 * definition (= 40) is never pulled, and g_tent reads as zero-initialized
 * COMMON storage. */

long g_tent;

void main(void) {
    output((int)g_tent);   /* 0 — archive member with g_tent=40 NOT pulled */
}
