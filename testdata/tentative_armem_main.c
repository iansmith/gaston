/* Main for the archive COMMON-index test: references g_common, whose only
 * definition is the tentative one in tentative_armem.c (inside an archive). */

extern long g_common;

void main(void) {
    g_common = 3;
    output((int)g_common);   /* 3 */
}
