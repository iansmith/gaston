/* Tentative definitions coexisting with real .bss (static) and .data
 * (initialized) objects in the same TU — COMMON allocation must not
 * overlap either region. */

static long s_arr[4];
long t_arr[4];
long init_v = 3;

void main(void) {
    s_arr[3] = 1;
    t_arr[3] = 2;
    output((int)s_arr[3]);  /* 1 */
    output((int)t_arr[3]);  /* 2 */
    output((int)s_arr[0]);  /* 0 */
    output((int)t_arr[0]);  /* 0 */
    output((int)init_v);    /* 3 */
}
