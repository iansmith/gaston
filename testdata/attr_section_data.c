/* Globals placed in custom data sections via __attribute__((section)) must
 * get real storage: reads see initial values, writes persist, and the
 * custom-section global must not alias normal .data neighbors. */

__attribute__((section(".mydata"))) int sec_val = 5;
__attribute__((section(".mydata"))) int sec_zero;
int normal = 3;

void main(void) {
    output(sec_val);       /* 5 */
    output(sec_zero);      /* 0 */
    sec_val = 9;
    output(sec_val);       /* 9 */
    output(normal);        /* 3 — untouched by the write */
}
