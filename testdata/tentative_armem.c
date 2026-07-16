/* Archive-member test payload: this TU's ONLY content is a tentative
 * definition. A program whose sole reference to g_common is an extern must
 * cause the archive member holding this COMMON to be pulled (the archive
 * symbol index must list COMMON symbols). */

long g_common;
