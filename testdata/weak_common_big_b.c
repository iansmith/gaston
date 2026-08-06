/* See weak_common_big_a.c. COMMON int bigv (4 bytes) vs A's weak long
 * (8 bytes); guard is adjacent COMMON storage whose value must survive. */
int bigv;
int guard;

void main(void) {
    guard = 9;
    output(bigv);  /* 0 — COMMON wins over the larger weak def */
    output(guard); /* 9 — neighboring COMMON slot uncorrupted */
}
