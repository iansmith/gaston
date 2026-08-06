/* Weak def vs COMMON, TU B (tentative def — no initializer, not weak). The
 * COMMON definition must win over A's weak def, so `val` is zero-initialized
 * storage, not 5. */
long val;

void main(void) {
    output((int)val); /* 0 — COMMON wins over weak-def per gABI */
}
