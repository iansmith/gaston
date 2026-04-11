/* attr_section_verify: symbols placed in custom ELF sections via
   __attribute__((section("name"))).  This file is used by TestAttrSectionELF
   in docker_test.go, which inspects the object file's section table directly
   rather than running the program.  The output() calls below allow the test
   to also run as a Docker integration test confirming correct execution. */
__attribute__((section(".text.hot")))
int hot_fn(void) { return 11; }

__attribute__((section(".text.cold")))
int cold_fn(void) { return 22; }

__attribute__((section(".rodata.cfg")))
const int cfg_val = 33;

int normal_fn(void) { return 44; }

void main(void) {
    output(hot_fn());    /* 11 */
    output(cold_fn());   /* 22 */
    output(cfg_val);     /* 33 */
    output(normal_fn()); /* 44 */
}
