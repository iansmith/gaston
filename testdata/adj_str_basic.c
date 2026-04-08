/* adj_str_basic.cm — adjacent string literals are concatenated by the lexer.
   Exercises: 2-part, 3-part, empty-prefix, empty-middle concatenation.
   Expected: "Hello, World!\nfoobarbaz\nnonempty\nAB\n" */

int main(void) {
    char *s;

    /* two parts */
    s = "Hello" ", World!\n";
    print_string(s);                       /* Hello, World! */

    /* three parts */
    print_string("foo" "bar" "baz\n");     /* foobarbaz */

    /* empty first segment */
    print_string("" "nonempty\n");         /* nonempty */

    /* empty middle segment */
    print_string("A" "" "B\n");            /* AB */

    return 0;
}
