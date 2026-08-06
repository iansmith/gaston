/* Two weak defs, no strong def: this TU only references `val` via extern so
 * link order among the two weak-defining TUs decides the winner. */
extern int val;

void main(void) {
    output(val);
}
