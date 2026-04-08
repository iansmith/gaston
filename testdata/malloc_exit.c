/* malloc_exit.cm — allocate with malloc, return 42 if non-null, 1 if null */
int main(void) {
    void *p;
    p = malloc(64);
    if (p != 0) {
        return 42;
    }
    return 1;
}
