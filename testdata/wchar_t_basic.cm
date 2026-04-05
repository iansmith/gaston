/* wchar_t_basic.cm — wchar_t is pre-registered in the lexer as unsigned int.
   Exercises: wchar_t scalar, array, arithmetic, sizeof, unsigned semantics.

   sizeof(wchar_t) == sizeof(unsigned int) == 4 on LP64.
   No sign extension: values ≥ 128 are stored and read back positive.
   Expected: 65 4 100 200 300 400 75 200 */

int main(void) {
    wchar_t w;
    wchar_t arr[4];
    int i;

    /* basic scalar */
    w = 65;
    output(w);               /* 65 */
    output(sizeof(unsigned int)); /* 4 — same size as wchar_t */

    /* array of wchar_t */
    for (i = 0; i < 4; i++) {
        arr[i] = (i + 1) * 100;
    }
    for (i = 0; i < 4; i++) {
        output(arr[i]);      /* 100 200 300 400 */
    }

    /* arithmetic */
    w = w + 10;
    output(w);               /* 75 */

    /* unsigned semantics: value > 127 stored without sign extension */
    w = 200;
    output(w);               /* 200 — not -56 (which char would give) */
    return 0;
}
