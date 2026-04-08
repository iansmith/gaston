/* sieve.cm — Sieve of Eratosthenes: output all primes <= 100 */

int flags[101];

void main(void) {
    int i;
    int j;
    int n;
    n = 100;
    i = 2;
    while (i <= n) {
        flags[i] = 1;
        i = i + 1;
    }
    i = 2;
    while (i <= n) {
        if (flags[i] == 1) {
            output(i);
            j = i + i;
            while (j <= n) {
                flags[j] = 0;
                j = j + i;
            }
        }
        i = i + 1;
    }
}
