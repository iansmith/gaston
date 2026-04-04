/* char_ptr_ptr.cm — char** double pointer: array of string pointers */
void main(void) {
    char *a;
    char *b;
    char *c;
    char **pp;
    a = "alpha\n";
    b = "beta\n";
    c = "gamma\n";
    pp = malloc(24);
    pp[0] = a;
    pp[1] = b;
    pp[2] = c;
    print_string(pp[0]);
    print_string(pp[1]);
    print_string(pp[2]);
    free(pp);
}
