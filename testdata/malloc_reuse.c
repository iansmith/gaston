int *p;
int main(void) {
    p = malloc(24);
    p[0] = 1;
    p[1] = 2;
    p[2] = 3;
    output(p[0]);
    free(p);
    p = malloc(24);
    p[0] = 10;
    p[1] = 20;
    p[2] = 30;
    output(p[2]);
    free(p);
    return 0;
}
