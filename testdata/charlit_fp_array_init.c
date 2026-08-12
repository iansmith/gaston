/* GAST-30: a global float array with char-literal elements must convert
 * each element, not store the raw char code as float bits. */

float arr[3] = {'a', 'b', 'c'};

void main(void) {
    output((int)arr[0]);   /* 97 */
    output((int)arr[1]);   /* 98 */
    output((int)arr[2]);   /* 99 */
}
