/* adj_str_escape.cm — adjacent string literals with escape sequences.
   Exercises: \n spanning a boundary, \t in multiple segments,
   escaped quote \" inside a concatenated literal.
   Expected: "line1\nline2\ncol1\tcol2\tcol3\nsay \"hi\"\n" */

int main(void) {
    char *s;

    /* \n at the end of first part, beginning of second (no blank line between) */
    s = "line1\n" "line2\n";
    print_string(s);                             /* line1<LF>line2<LF> */

    /* \t separating columns across three segments */
    print_string("col1\t" "col2\t" "col3\n");    /* col1<TAB>col2<TAB>col3 */

    /* escaped quote in concatenated literal */
    print_string("say " "\"hi\"\n");             /* say "hi" */

    return 0;
}
