/* GAST-27: a global float array initialized with integer-literal elements
 * must convert each element, not store raw integer bits. */

float a[3] = {1, 2, 3};

void main(void) {
    output((int)a[0]);   /* 1 */
    output((int)a[1]);   /* 2 */
    output((int)a[2]);   /* 3 */
}
