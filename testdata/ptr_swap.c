/* ptr_swap.cm — classic swap via two int* parameters */
void swap(int *a, int *b) {
    int tmp;
    tmp = *a;
    *a = *b;
    *b = tmp;
}

void main(void) {
    int x, y;
    x = 7;
    y = 3;
    swap(&x, &y);
    output(x);
    output(y);
}
