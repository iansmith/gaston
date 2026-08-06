/* Adversary gap (round 2, finding 2): UNINITIALIZED weak def — objgen routes
 * this down a third path (STB_WEAK in .bss), distinct from initialized-weak
 * .data and from COMMON. */
__attribute__((weak)) int zed;
