/* sizeof_int_abi.cm — verifies LP64 sizeof correctness: sizeof(int)=4, sizeof(double)=8.
   Note: gaston maps 'long' to TypeInt (4 bytes); use 'double' to test 8-byte types.

   struct ISize { int a; int b; }  — a@0(4), b@4(4), size=8

   Expected: 4 8 8 */

struct ISize { int a; int b; };

void main(void) {
    output(sizeof(int));           /* 4 — LP64 C ABI */
    output(sizeof(double));        /* 8 — 64-bit FP */
    output(sizeof(struct ISize));  /* 8 — 2 * 4; LP64 C ABI */
}
