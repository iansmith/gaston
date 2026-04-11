/* attr_alias: __attribute__((alias("sym"))) makes one symbol a link-time
   alias for another.  Both names must call the same underlying code. */
int original(void) { return 42; }
int aliased(void) __attribute__((alias("original")));

/* Variable alias: aliased_val refers to the same storage as original_val */
int original_val = 7;
extern int aliased_val __attribute__((alias("original_val")));

void main(void) {
    output(original());               /* 42 */
    output(aliased());                /* 42 — same function, different name */
    output(original() == aliased());  /* 1 */

    output(original_val);             /* 7 */
    output(aliased_val);              /* 7 — same storage */
    aliased_val = 99;
    output(original_val);             /* 99 — write through alias visible on original */
}
