/* GAST-30: a struct field that is itself a float array must convert
 * int-literal initializer elements, both for a global and a local struct
 * instance. Root cause: the nested-init synthetic decl built for the
 * recursive struct-field-array walk lost the array's element type. */

struct S { float arr[3]; };

struct S g = {{1, 2, 3}};

void main(void) {
    output((int)g.arr[0]);      /* 1 */
    output((int)g.arr[1]);      /* 2 */
    output((int)g.arr[2]);      /* 3 */

    struct S s = {{4, 5, 6}};
    output((int)s.arr[0]);      /* 4 */
    output((int)s.arr[1]);      /* 5 */
    output((int)s.arr[2]);      /* 6 */
}
