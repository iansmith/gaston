/* An initialized global pointer whose target is a tentative-defined object.
 * This exercises the .rela.data ABS64 relocation path against a COMMON
 * symbol (the in-function uses exercise only .rela.text). */

long tv;
long *ptr = &tv;

void main(void) {
    tv = 6;
    output((int)*ptr);          /* 6 */
    output((int)(ptr == &tv));  /* 1 */
}
