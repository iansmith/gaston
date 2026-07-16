/* Function-pointer parameters with pointer return types, named and abstract —
 * the pthread_create prototype shape: void *(*)(void *). */

int run(void *(*fn)(void *), void *arg);
int run2(void *(*)(void *), void *);

void *doubler(void *p) {
    long *v = p;
    *v = *v * 2;
    return p;
}

int run(void *(*fn)(void *), void *arg) {
    long *r = fn(arg);
    return (int)*r;
}

void main(void) {
    long x = 21;
    output(run(doubler, &x));   /* 42 */
}
