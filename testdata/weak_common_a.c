/* Weak def vs COMMON (tentative def) of the same name, TU A (weak def). Per
 * ELF gABI: "if a defined global symbol exists, then references to the weak
 * symbol are treated as if the global symbol had been used... if a common
 * symbol exists, the link editor honors the common definition and ignores
 * the weak ones." Precedence: strong > COMMON > weak-def. */
__attribute__((weak)) long val = 5;
