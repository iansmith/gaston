/* __attribute__((section)) is parsed and ignored; symbol placed in default section */
__attribute__((section(".text.special"))) int special_fn(void) { return 77; }

void main(void) {
    output(special_fn());  /* 77 */
}
