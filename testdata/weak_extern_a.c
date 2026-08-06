/* Cross-TU weak definition, TU A. `flag` is a weak global definition; TU B
 * references it only via `extern`. GAST-8: today the linker's symVA build
 * loop only admits STB_GLOBAL symbols, so this weak def never enters the
 * global symbol table and B's extern reference resolves to VA 0. */
__attribute__((weak)) int flag = 7;
