/* switch_continue.cm — continue inside switch targets enclosing loop,
 * not the switch itself.  Covers do-while, while, and for loops.
 *
 * Expected output:
 *   10  (do-while: case 1 sets acc=10, continue re-enters loop)
 *   20  (do-while: case 2 adds 10 → acc=20, continue re-enters)
 *   20  (do-while: case 3 hits default which breaks, acc still 20)
 *   60  (while: sum of values that aren't 3 → 1+2+4+5+6+7+8+9+10=52... no)
 *   52  (while: 1+2+4+5+6+7+8+9+10 = 52)
 *   20  (for: counts non-skip values 1..10 skipping 3,6,9 → 7 items... no)
 *
 * Let's keep it simple.
 */

void main(void) {
    int i;
    int acc;

    /* --- do-while with switch and continue --- */
    i = 0;
    acc = 0;
    do {
        i = i + 1;
        switch (i) {
        case 1:
            acc = acc + 10;
            continue;  /* should go back to do-while, not break switch */
        case 2:
            acc = acc + 10;
            continue;
        default:
            break;     /* breaks the switch, falls to while-condition */
        }
        /* If continue works, we skip this line for cases 1 and 2 */
        acc = acc + 100;
    } while (i < 3);
    output(acc);  /* expect: 120 → 10+10+100 = 120 */

    /* --- while with switch and continue --- */
    i = 0;
    acc = 0;
    while (i < 5) {
        i = i + 1;
        switch (i) {
        case 3:
            continue;  /* skip adding for i==3 */
        default:
            break;
        }
        acc = acc + i;
    }
    output(acc);  /* expect: 1+2+4+5 = 12 */

    /* --- for with switch and continue --- */
    acc = 0;
    for (i = 1; i <= 5; i = i + 1) {
        switch (i) {
        case 2:
            continue;  /* skip i==2 */
        case 4:
            continue;  /* skip i==4 */
        default:
            break;
        }
        acc = acc + i;
    }
    output(acc);  /* expect: 1+3+5 = 9 */
}
