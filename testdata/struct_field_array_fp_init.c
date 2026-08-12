/* GAST-30: a struct field that is itself an FP array must convert
 * int-literal initializer elements, both for a global and a local struct
 * instance. Root cause: the nested-init synthetic decl built for the
 * recursive struct-field-array walk lost the array's element type.
 *
 * Uses "double arr[3]" rather than "float arr[3]" deliberately: a struct
 * field array's byte size is computed from the real per-element ABI size
 * (fieldSizeAlign), but every array element WRITE in this compiler uses a
 * uniform 8-byte stride regardless of declared element type (matching how
 * top-level arrays already work). For "double" those agree (8 bytes each
 * way); for "float" they don't (4 vs 8), which undersizes the field's
 * storage independent of this ticket's ElemType-propagation fix — a
 * separate, deeper bug, filed on its own rather than folded in here. */

struct S { double arr[3]; };

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
