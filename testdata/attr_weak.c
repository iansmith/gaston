/* Weak function: uses the weak definition since no override at link time */
__attribute__((weak)) int weak_cb(void) { return 99; }

void main(void) {
    output(weak_cb());  /* 99 */
}
