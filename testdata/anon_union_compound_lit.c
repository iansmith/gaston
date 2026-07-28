/* Anonymous union compound literal used inline — the asuint64-shaped bit
 * twiddling pattern from musl's libm.h:
 * ((union{double f; uint64_t i;}){3.0}).i
 * Also exercise the anonymous struct compound-literal form (no pointer),
 * which previously only existed as a cast target. */

void main(void) {
    unsigned long bits = ((union{double f; unsigned long i;}){3.0}).i;
    output((int)(bits != 0)); /* 1 */

    int sum = ((struct{int a; int b;}){10, 32}).a + ((struct{int a; int b;}){10, 32}).b;
    output(sum); /* 42 */
}
