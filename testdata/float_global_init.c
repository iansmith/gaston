/* GAST-28: a global scalar "float" (4-byte) variable with a compile-time
 * initializer must read back correctly. gaston stores every FP value as a
 * double internally (locals, params, array elements all use 8-byte D-register
 * loads/stores uniformly) — the global-scalar-float initializer path was the
 * one place writing a packed 4-byte IEEE754-single bit pattern instead. */

float g = 3.0;
double d = 5.0;
long double ld = 7.0;
float arr[3] = {1.0, 2.0, 3.0};

void main(void) {
    output((int)g);        /* 3 */
    output((int)d);        /* 5 (control: already worked) */
    output((int)ld);       /* 7 (control: already worked) */
    output((int)arr[2]);   /* 3 (control: already worked) */
}
