/* malloc_basic.cm — allocate an array with malloc, fill it, print it, free it */
int i;
int *p;

int main(void) {
    p = malloc(80);
    i = 0;
    while (i < 10) {
        p[i] = i * i;
        i = i + 1;
    }
    i = 0;
    while (i < 10) {
        output(p[i]);
        i = i + 1;
    }
    free(p);
    return 0;
}
