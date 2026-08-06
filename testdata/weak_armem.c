/* Sole definition of g_weak is a weak def inside an archive member with no
 * other symbol referenced by the main program. The archive symbol index
 * must list weak-bound symbols, or the member is never pulled and the
 * extern reference in weak_armem_main.c resolves to nothing. */
__attribute__((weak)) int g_weak = 3;
