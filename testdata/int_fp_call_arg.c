/* GAST-27: an int value (literal or variable) passed to a float/double/
 * long-double parameter must promote, not silently misroute through the
 * wrong register class. */

double id_d(double z) { return z; }
float id_f(float z) { return z; }
long double id_ld(long double z) { return z; }

void main(void) {
    int i = 3;
    output((int)id_d(2));       /* 2: int literal -> double param */
    output((int)id_f(3));       /* 3: int literal -> float param */
    output((int)id_ld(4));      /* 4: int literal -> long double param */
    output((int)id_f(i));       /* 3: int variable -> float param */
}
