/* Archive-member test payload: a REAL (initialized) definition of a symbol
 * that the main object defines tentatively. Per classic linker semantics a
 * tentative definition in a user object SATISFIES the symbol — this member
 * must NOT be pulled just to override it. */

long g_tent = 40;
