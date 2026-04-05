/* vla_sum.cm — VLA sized at runtime, filled in a loop, passed to a function.
   Byzantine test for item 13: VLA allocation, element write, pass-by-name
   (array decay), sum returned from a separate function.
   Two calls with different sizes prove the stack frame is managed correctly.

   Note: pass arr (not &arr) — array name decays to base pointer via IRGetAddr.

   Expected: 55 15 4 */

int sum_arr(int *p, int n) {
    int s;
    int i;
    s = 0;
    i = 0;
    while (i < n) {
        s = s + p[i];
        i = i + 1;
    }
    return s;
}

int fill_and_sum(int n) {
    int arr[n];
    int i;
    i = 0;
    while (i < n) {
        arr[i] = i + 1;
        i = i + 1;
    }
    return sum_arr(arr, n);
}

void main(void) {
    output(fill_and_sum(10));   /* 1+2+...+10 = 55 */
    output(fill_and_sum(5));    /* 1+2+3+4+5 = 15 */
    output(sizeof(int));        /* 4 — LP64 */
}
