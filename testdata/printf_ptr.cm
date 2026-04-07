/* printf_ptr.cm — test P2-B: pointer format with zero-padded hex.
 * picolibc's %p prints minimal hex (no zero-padding), matching glibc.
 * Use explicit %016lx for consistent 16-digit zero-padded output.
 */
#include <stdio.h>

int main(void) {
    char* p;
    long addr;

    /* known small address: cast int to pointer */
    addr = 42;
    p = (char*)addr;
    printf("0x%016lx\n", (unsigned long)p);

    /* NULL pointer */
    p = (char*)0;
    printf("0x%016lx\n", (unsigned long)p);

    /* larger address */
    addr = 0xdeadbeef;
    p = (char*)addr;
    printf("0x%016lx\n", (unsigned long)p);

    return 0;
}
