/* ptr_void.cm — void* accepts any pointer type in assignment */
void fill(void *buf, int n) {
    int *p;
    int i;
    p = buf;
    i = 0;
    while (i < n) {
        p[i] = i * 2;
        i = i + 1;
    }
}

void main(void) {
    int *p;
    p = malloc(24);
    fill(p, 3);
    output(p[0]);
    output(p[1]);
    output(p[2]);
    free(p);
}
