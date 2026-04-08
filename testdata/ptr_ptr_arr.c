/* ptr_ptr_arr.cm — int** subscript via malloc'd array of pointers */
void main(void) {
    int x;
    int y;
    int z;
    int **pp;
    int *tmp;
    x = 10;
    y = 20;
    z = 30;
    pp = malloc(24);
    pp[0] = &x;
    pp[1] = &y;
    pp[2] = &z;
    output(*pp[0]);
    output(*pp[1]);
    output(*pp[2]);
    tmp = pp[1];
    output(*tmp);
    free(pp);
}
