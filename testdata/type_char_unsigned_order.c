/* Reversed type-specifier order: `unsigned` after `char` instead of before.
 * C permits type-specifier keywords in any order; gaston already accepts
 * `unsigned char` but not `char unsigned` (musl's __map_file.c uses this). */

void main(void) {
    char unsigned x = 200;
    unsigned char y = 200;
    output((int)x); /* 200 */
    output((int)y); /* 200 */
}
