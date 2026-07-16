/* Pure-extern reference resolved by a tentative definition in another TU.
 * TU A holds the only definition (tentative); TU B references it via extern. */

long tvar;

long get_tvar(void);

void main(void) {
    tvar = 5;
    output((int)get_tvar());   /* 5 */
}
