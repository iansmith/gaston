/* alignas_global: global variables with _Alignas(N) for ELF inspection.
   TestAlignasGlobalELF in docker_test.go checks that symbol values within
   the .data and .bss sections are multiples of the requested alignment.    */

int normal_var = 1;              /* natural 8-byte alignment */
_Alignas(16) int aligned16 = 2; /* 16-byte aligned in .data */
char sep1 = 3;                   /* breaks natural alignment stream */
_Alignas(32) int aligned32 = 4; /* 32-byte aligned in .data */

_Alignas(16) int bss_aligned;   /* 16-byte aligned in .bss */

void main(void) {
    output(normal_var + aligned16 + sep1 + aligned32 + bss_aligned);
}
