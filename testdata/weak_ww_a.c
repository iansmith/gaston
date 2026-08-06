/* Two weak defs, no strong def, TU A. With no strong definition anywhere,
 * the first weak definition encountered in link order must win. */
__attribute__((weak)) int val = 11;
